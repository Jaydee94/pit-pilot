/// <reference lib="webworker" />
import { precacheAndRoute } from "workbox-precaching";

declare let self: ServiceWorkerGlobalScope;

precacheAndRoute(self.__WB_MANIFEST);

self.addEventListener("push", (event: PushEvent) => {
  const data = (event.data && event.data.json()) || {};
  event.waitUntil(
    self.registration.showNotification(data.title || "pit-pilot", {
      body: data.body,
      data: { url: data.url || "/" },
    }),
  );
});

self.addEventListener("notificationclick", (event: NotificationEvent) => {
  event.notification.close();
  const url = (event.notification.data && event.notification.data.url) || "/";
  event.waitUntil(
    (async () => {
      const wins = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
      for (const c of wins) {
        await (c as WindowClient).focus();
        await (c as WindowClient).navigate(url);
        return;
      }
      await self.clients.openWindow(url);
    })(),
  );
});
