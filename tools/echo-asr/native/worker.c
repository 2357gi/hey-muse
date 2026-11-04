// Persistent CPU inference worker. Only framed PCM enters; only UTF-8 leaves.
#include "c-api.h"
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <time.h>

static double now(void) {
  struct timespec t;
  clock_gettime(CLOCK_MONOTONIC, &t);
  return t.tv_sec + t.tv_nsec / 1e9;
}
int main(int argc, char **argv) {
  if (argc != 3)
    return 2;
  char encoder[512], decoder[512], tokens[512];
  snprintf(encoder, sizeof encoder, "%s/encoder_model.ort", argv[1]);
  snprintf(decoder, sizeof decoder, "%s/decoder_model_merged.ort", argv[1]);
  snprintf(tokens, sizeof tokens, "%s/tokens.txt", argv[1]);
  SherpaOnnxOfflineRecognizerConfig c = {0};
  c.decoding_method = "greedy_search";
  c.model_config.provider = "cpu";
  c.model_config.num_threads = atoi(argv[2]);
  c.model_config.tokens = tokens;
  c.model_config.moonshine.encoder = encoder;
  c.model_config.moonshine.merged_decoder = decoder;
  const SherpaOnnxOfflineRecognizer *r = SherpaOnnxCreateOfflineRecognizer(&c);
  if (!r)
    return 3;
  puts("READY");
  fflush(stdout);
  uint32_t length;
  while (fread(&length, 4, 1, stdin) == 1) {
    if (length > 960000 || length % 2 || length < 2)
      return 4;
    int16_t *pcm = malloc(length);
    float *samples = malloc(length / 2 * sizeof(float));
    if (!pcm || !samples)
      return 5;
    if (fread(pcm, 1, length, stdin) != length)
      return 6;
    for (uint32_t i = 0; i < length / 2; i++)
      samples[i] = pcm[i] / 32768.0f;
    double start = now();
    const SherpaOnnxOfflineStream *s = SherpaOnnxCreateOfflineStream(r);
    if (!s)
      return 7;
    SherpaOnnxAcceptWaveformOffline(s, 16000, samples, length / 2);
    SherpaOnnxDecodeOfflineStream(r, s);
    const SherpaOnnxOfflineRecognizerResult *result =
        SherpaOnnxGetOfflineStreamResult(s);
    if (!result || !result->text)
      return 8;
    uint32_t outlen = (uint32_t)strlen(result->text);
    if (outlen > 16384)
      return 9;
    fwrite(&outlen, 4, 1, stdout);
    fwrite(result->text, 1, outlen, stdout);
    fflush(stdout);
    struct rusage usage;
    getrusage(RUSAGE_SELF, &usage);
    fprintf(stderr,
            "audio_seconds=%.3f processing_seconds=%.3f max_rss_kb=%ld\n",
            length / 32000.0, now() - start, usage.ru_maxrss);
    SherpaOnnxDestroyOfflineRecognizerResult(result);
    SherpaOnnxDestroyOfflineStream(s);
    free(samples);
    free(pcm);
  }
  SherpaOnnxDestroyOfflineRecognizer(r);
  return 0;
}
