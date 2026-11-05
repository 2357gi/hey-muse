"""Warm GATT connection to the cloud-identified hallway Color Bulb only."""
import asyncio
import concurrent.futures
import threading
from bleak import BleakClient, BleakScanner

SERVICE = 'cba20d00-224d-11e6-9fb8-0002a5d5c51b'
WRITE = 'cba20002-224d-11e6-9fb8-0002a5d5c51b'
NOTIFY = 'cba20003-224d-11e6-9fb8-0002a5d5c51b'
POWER = {'on':bytes.fromhex('570f470101'),'off':bytes.fromhex('570f470102')}
INFO = bytes.fromhex('570f4801')


class BulbLink:
    def __init__(self, ident):
        if len(ident)!=12 or len(bytes.fromhex(ident))!=6:
            raise ValueError('invalid bulb address')
        self.address=':'.join(ident[i:i+2] for i in range(0,12,2))
        self.ready=threading.Event()
        self.loop=None
        self.client=None
        self.pending=None
        self.gate=None
        self.lost=None

    def notify(self, _, value):
        if self.pending is not None and not self.pending.done():
            self.pending.set_result(bytes(value))

    async def exchange(self, command):
        async with self.gate:
            if not self.client or not self.client.is_connected or self.lost.is_set():
                raise ConnectionError('bulb disconnected')
            self.pending=asyncio.get_running_loop().create_future()
            try:
                await asyncio.wait_for(self.client.write_gatt_char(WRITE,command,response=False),1.5)
                reply=await asyncio.wait_for(self.pending,1.5)
                if not reply or reply[0]!=1:
                    raise ValueError('bulb did not accept command')
                return reply
            except BaseException:
                # A late notification must not become a receipt for a different request.
                self.ready.clear()
                self.lost.set()
                raise
            finally:
                self.pending=None

    async def power(self):
        data=await self.exchange(INFO)
        if len(data)<11:raise ValueError('invalid bulb status')
        return 'on' if data[1]&128 else 'off'

    async def connected(self):
        dev=await BleakScanner.find_device_by_address(self.address,timeout=8)
        if dev is None:raise ConnectionError('target not advertising')
        lost=self.lost=asyncio.Event()
        self.gate=asyncio.Lock()
        async with BleakClient(dev,disconnected_callback=lambda _:lost.set(),timeout=8) as client:
            service=client.services.get_service(SERVICE)
            if not service or not service.get_characteristic(WRITE) or not service.get_characteristic(NOTIFY):
                raise ValueError('unexpected GATT service')
            self.client=client
            try:
                def on_notification(sender, value):
                    if self.client is client and not lost.is_set():
                        self.notify(sender,value)
                await client.start_notify(NOTIFY,on_notification)
                await self.power()
                self.ready.set()
                await lost.wait()
            finally:
                self.ready.clear()
                self.client=None

    async def supervise(self):
        self.loop=asyncio.get_running_loop()
        while True:
            try:await self.connected()
            except Exception:pass
            await asyncio.sleep(5)

    def start(self):
        threading.Thread(target=lambda:asyncio.run(self.supervise()),daemon=True,name='hall-ble').start()

    def set_power(self,direction):
        # False means no write was attempted: only then may the caller use HTTPS.
        if not self.ready.is_set():return False
        task=asyncio.run_coroutine_threadsafe(self.exchange(POWER[direction]),self.loop)
        try:task.result(timeout=3.5)
        except concurrent.futures.TimeoutError:
            task.cancel()
            raise
        return True
