// On-device Silero VAD. One Unix-socket connection is one utterance.
#include "c-api.h"
#include <errno.h>
#include <math.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/un.h>
#include <time.h>
#include <unistd.h>

static double now(void) {
  struct timespec t;
  clock_gettime(CLOCK_MONOTONIC, &t);
  return t.tv_sec + t.tv_nsec / 1e9;
}
static int read_all(int fd, void *p, size_t n) {
  size_t at = 0;
  while (at < n) {
    ssize_t k = read(fd, (char *)p + at, n - at);
    if (k < 0 && errno == EINTR)
      continue;
    if (k <= 0)
      return 0;
    at += (size_t)k;
  }
  return 1;
}
static int write_all(int fd, const void *p, size_t n) {
  size_t at = 0;
  while (at < n) {
    ssize_t k = write(fd, (const char *)p + at, n - at);
    if (k < 0 && errno == EINTR)
      continue;
    if (k <= 0)
      return 0;
    at += (size_t)k;
  }
  return 1;
}
static float scaled(float x, float gain) {
  x *= gain;
  return fmaxf(-1.f, fminf(1.f, x));
}
static void result(const SherpaOnnxVoiceActivityDetector *v, uint32_t *out) {
  out[0] = SherpaOnnxVoiceActivityDetectorDetected(v) ? 1 : 0;
  out[1] = 0;
  if (!SherpaOnnxVoiceActivityDetectorEmpty(v)) {
    const SherpaOnnxSpeechSegment *s = SherpaOnnxVoiceActivityDetectorFront(v);
    if (s && s->start >= 0) {
      out[0] |= 2;
      out[1] = (uint32_t)(s->start + s->n);
    }
    if (s)
      SherpaOnnxDestroySpeechSegment(s);
  }
}
int main(int argc, char **argv) {
  if (argc < 3 || argc > 5) {
    fprintf(stderr,
            "usage: vad-service model socket | model --wav file [gain]\n");
    return 2;
  }
  signal(SIGPIPE, SIG_IGN);
  float gain = argc == 5 ? strtof(argv[4], NULL) : 8.f;
  SherpaOnnxVadModelConfig c = {0};
  c.silero_vad.model = argv[1];
  c.silero_vad.threshold = .35f;
  c.silero_vad.min_silence_duration = .8f;
  c.silero_vad.min_speech_duration = .2f;
  c.silero_vad.window_size = 512;
  c.silero_vad.max_speech_duration = 30.f;
  c.sample_rate = 16000;
  c.num_threads = 1;
  c.provider = "cpu";
  const SherpaOnnxVoiceActivityDetector *v =
      SherpaOnnxCreateVoiceActivityDetector(&c, 32.f);
  if (!v)
    return 3;
  if (argc >= 4 && strcmp(argv[2], "--wav") == 0) {
    const SherpaOnnxWave *w = SherpaOnnxReadWave(argv[3]);
    if (!w)
      return 4;
    int n = (int)((int64_t)w->num_samples * 16000 / w->sample_rate);
    float frame[512];
    double began = now();
    uint32_t out[2] = {0};
    int at = 0, first_speech = -1;
    for (; at < n; at += 512) {
      int count = n - at < 512 ? n - at : 512;
      for (int i = 0; i < count; i++) {
        double pos = (at + i) * (double)w->sample_rate / 16000;
        int j = (int)pos;
        int k = j + 1 < w->num_samples ? j + 1 : j;
        frame[i] = scaled(w->samples[j] + (w->samples[k] - w->samples[j]) *
                                              (float)(pos - j),
                          gain);
      }
      SherpaOnnxVoiceActivityDetectorAcceptWaveform(v, frame, count);
      result(v, out);
      if ((out[0] & 1) && first_speech < 0)
        first_speech = at + count;
      if (out[0] & 2) {
        at += count;
        break;
      }
    }
    struct rusage r;
    getrusage(RUSAGE_SELF, &r);
    printf("{\"ended\":%s,\"decision_seconds\":%.3f,\"speech_end_seconds\":%."
           "3f,\"first_speech_seconds\":%.3f,\"processing_seconds\":%.3f,\"max_"
           "rss_kib\":%ld,\"gain\":%.1f}\n",
           out[0] & 2 ? "true" : "false", at / 16000., out[1] / 16000.,
           first_speech / 16000., now() - began, r.ru_maxrss, gain);
    SherpaOnnxFreeWave(w);
    SherpaOnnxDestroyVoiceActivityDetector(v);
    return 0;
  }
  if (argc != 3 ||
      strlen(argv[2]) >= sizeof(((struct sockaddr_un *)0)->sun_path))
    return 5;
  int s = socket(AF_UNIX, SOCK_STREAM, 0);
  if (s < 0)
    return 6;
  struct sockaddr_un address = {0};
  address.sun_family = AF_UNIX;
  strcpy(address.sun_path, argv[2]);
  // Refuse to replace an active service socket; only remove an abandoned one.
  if (access(argv[2], F_OK) == 0) {
    int probe = socket(AF_UNIX, SOCK_STREAM, 0);
    int live = connect(probe, (struct sockaddr *)&address, sizeof address) == 0;
    close(probe);
    if (live)
      return 7;
    unlink(argv[2]);
  }
  umask(0077);
  if (bind(s, (struct sockaddr *)&address, sizeof address) || listen(s, 1))
    return 8;
  fprintf(
      stderr,
      "ready socket=%s threshold=0.35 silence_seconds=0.8 gain=8 threads=1\n",
      argv[2]);
  for (;;) {
    int client = accept(s, NULL, NULL);
    if (client < 0) {
      if (errno == EINTR)
        continue;
      break;
    }
    struct timeval timeout = {2, 0};
    setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof timeout);
    setsockopt(client, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof timeout);
    SherpaOnnxVoiceActivityDetectorReset(v);
    uint32_t length, total = 0, out[2] = {0};
    double compute = 0;
    int16_t pcm[1600];
    float samples[1600];
    while (read_all(client, &length, 4)) {
      if (length == 0 || length > sizeof pcm || length % 2 ||
          total + length / 2 > 30 * 16000)
        break;
      if (!read_all(client, pcm, length))
        break;
      for (uint32_t i = 0; i < length / 2; i++)
        samples[i] = scaled(pcm[i] / 32768.f, gain);
      double began = now();
      SherpaOnnxVoiceActivityDetectorAcceptWaveform(v, samples, length / 2);
      result(v, out);
      compute += now() - began;
      total += length / 2;
      if (!write_all(client, out, sizeof out))
        break;
      if (out[0] & 2)
        break;
    }
    if (total) {
      struct rusage r;
      getrusage(RUSAGE_SELF, &r);
      fprintf(stderr,
              "audio_seconds=%.3f ended=%u speech_end_seconds=%.3f "
              "processing_seconds=%.3f max_rss_kib=%ld\n",
              total / 16000., !!(out[0] & 2), out[1] / 16000., compute,
              r.ru_maxrss);
    }
    close(client);
  }
  close(s);
  unlink(argv[2]);
  SherpaOnnxDestroyVoiceActivityDetector(v);
  return 0;
}
