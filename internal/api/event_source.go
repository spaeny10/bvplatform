package api

import (
	"context"
	"log"
	"strings"

	"onvif-tool/internal/database"
	"onvif-tool/internal/drivers"
	msdriver "onvif-tool/internal/milesight"
	"onvif-tool/internal/onvif"
)

// StartCameraEventSource attaches a vendor-appropriate event stream to
// an already-built EventSubscriber. Milesight cameras use the
// proprietary /webstream/track WebSocket — their ONVIF PullPoint impl
// silently drops events past a low subscription cap. Other cameras use
// PullPoint with driver-specific Classify/Enrich hooks if registered.
func StartCameraEventSource(
	ctx context.Context,
	cam *database.Camera,
	subscriber *onvif.EventSubscriber,
	subReg *SubscriberRegistry,
	label string,
) {
	if subscriber == nil || cam == nil {
		return
	}

	isMilesight := strings.Contains(strings.ToLower(cam.Manufacturer), "milesight")
	if isMilesight && cam.OnvifAddress != "" {
		msCam := msdriver.New(cam.OnvifAddress, cam.Username, cam.Password)
		camIDForMS := cam.ID
		msStream := msdriver.NewEventStream(msCam, cam.Name, func(eventType string, metadata map[string]interface{}) {
			subscriber.InjectEvent(camIDForMS, eventType, metadata)
		})
		// Probe once before committing. The /webstream/track endpoint is
		// only on newer Milesight firmware; older / Pro Series cameras
		// 404 the upgrade and would otherwise loop reconnecting forever
		// while ONVIF PullPoint never starts.
		if err := msStream.Probe(ctx); err == nil {
			msStream.Start(ctx)
			log.Printf("[%s] Camera %s: Milesight WebSocket event stream active (ONVIF PullPoint skipped)",
				label, cam.Name)
			return
		} else {
			log.Printf("[%s] Camera %s: Milesight WebSocket probe failed (%v) — falling back to ONVIF PullPoint",
				label, cam.Name, err)
		}
	}

	// ONVIF PullPoint path — the universal fallback. Picks up driver
	// hooks if a vendor driver is registered.
	info := &onvif.DeviceInfo{Manufacturer: cam.Manufacturer, Model: cam.Model}
	if drv := drivers.ForDevice(info); drv != nil {
		subscriber.Classify = drv.ClassifyEvent
		subscriber.Enrich = drv.EnrichEvent
		log.Printf("[%s] Camera %s: %s driver attached for events", label, cam.Name, drv.Name())
	}
	subscriber.Start(ctx)
	if subReg != nil {
		subReg.Register(cam.ID, subscriber)
	}
	log.Printf("[%s] Camera %s: ONVIF events subscription active", label, cam.Name)
}
