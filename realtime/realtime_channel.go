package realtime

import "fmt"

type realtimeTopic string // internal string type for representing topics

type RealtimeChannel struct {
	topic     realtimeTopic
	client    *RealtimeClient
	hasJoined bool
}

// Initialize a new channel
func CreateRealtimeChannel(client *RealtimeClient, topic realtimeTopic) *RealtimeChannel {
	return &RealtimeChannel{
		client: client,
		topic:  topic,
	}
}

// Perform callbacks on specific events. Successive calls to On()
// will result in multiple callbacks acting at the event
func (channel *RealtimeChannel) On() {

}

// Subscribe to the channel and start listening to events
func (channel *RealtimeChannel) Subscribe() error {
	if channel.hasJoined {
		return fmt.Errorf("The channel has already been subscribed")
	}

	if channel.client.isClientAlive() {

	}

	return nil
}

func (channel *RealtimeChannel) Unsubscribe() {
	if channel.client.isClientAlive() {

	}
}
