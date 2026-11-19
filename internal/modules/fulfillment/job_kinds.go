package fulfillment

import "app.local/internal/modules/order/events"

func JobKinds() []string {
	return []string{events.OrderCreated{}.Kind()}
}
