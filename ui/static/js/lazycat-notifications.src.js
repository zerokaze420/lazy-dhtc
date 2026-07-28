import { lzcAPIGateway } from "@lazycatcloud/sdk";

const gateway = new lzcAPIGateway("/_lzc/runtime/grpc/");
let currentDevicePromise;
let unavailable = false;

async function getCurrentDevice() {
  if (unavailable) {
    return null;
  }
  if (!currentDevicePromise) {
    currentDevicePromise = gateway.currentDevice.catch((error) => {
      unavailable = true;
      console.debug("Lazycat system notification unavailable", error);
      return null;
    });
  }
  return currentDevicePromise;
}

async function notify({ title, body, deeplinkUrl }) {
  const device = await getCurrentDevice();
  if (!device?.notification?.Notify) {
    return false;
  }

  try {
    await device.notification.Notify({
      title: title || "dhtc",
      body: body || "",
      deeplinkUrl: deeplinkUrl || window.location.href,
    });
    return true;
  } catch (error) {
    console.debug("Lazycat system notification failed", error);
    return false;
  }
}

window.dhtcNotifySystem = notify;
