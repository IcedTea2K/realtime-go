package realtime

import (
	"encoding/json"
)

type TemplateMsg struct {
	Event string `json:"event"`
	Topic string `json:"topic"`
	Ref   string `json:"ref"`
}

type AbstractMsg struct {
	*TemplateMsg
	Payload json.RawMessage `json:"payload"`
}

type ConnectionMsg struct {
	*TemplateMsg

	Payload struct {
		Config struct {
			Broadcast struct {
				Self bool `json:"self"`
			} `json:"broadcast,omitempty"`

			Presence struct {
				Key string `json:"key"`
			} `json:"presence,omitempty"`

			PostgresChanges []postgresFilter `json:"postgres_changes,omitempty"`
		} `json:"config"`
	} `json:"payload"`
}

type PostgresCDCMsg struct {
	*TemplateMsg

	Payload struct {
		Data struct {
			Schema     string            `json:"schema"`
			Table      string            `json:"table"`
			CommitTime string            `json:"commit_timestamp"`
			EventType  string            `json:"eventType"`
			New        map[string]string `json:"new"`
			Old        map[string]string `json:"old"`
			Errors     string            `json:"errors"`
		} `json:"data"`
	} `json:"payload"`
}

type ReplyPayload struct {
   Response struct {
      PostgresChanges []struct{
         ID int `json:"id"`
         postgresFilter
      } `json:"postgres_changes"`
   } `json:"response"`
   Status string `json:"status"`
}

type SystemPayload struct {
   Channel     string `json:"channel"`
   Extension   string `json:"extension"`
   Message     string `json:"message"`
   Status      string `json:"status"`
}

type PostgresCDCPayload struct {
   Data struct {
      Schema     string            `json:"schema"`
      Table      string            `json:"table"`
      CommitTime string            `json:"commit_timestamp"`
      EventType  string            `json:"eventType"`
      New        map[string]string `json:"new"`
      Old        map[string]string `json:"old"`
      Errors     string            `json:"errors"`
   } `json:"data"`
   IDs []int `json:"ids"`
}

// presence_state can contain any key. Hence map type instead of struct
type PresenceStatePayload map[string]struct{
   Metas []struct{
      Ref   string   `json:"phx_ref"` 
      Name  string   `json:"name"` 
      T     float64  `json:"t"` 
   } `json:"metas,omitempty"`
}

type HearbeatMsg struct {
	*TemplateMsg

	Payload struct {
	} `json:"payload"`
}

// create a template message
func createTemplateMessage(event string, topic string) *TemplateMsg {
	return &TemplateMsg{
		Event: event,
		Topic: topic,
		Ref:   "",
	}
}

// create a connection message depending on event type
func createConnectionMessage(topic string, filter eventFilter) *ConnectionMsg {
	msg := &ConnectionMsg{}

	// Common part across the three event type
	msg.TemplateMsg = createTemplateMessage(joinEvent, topic)
	switch filter.(type) {
	case postgresFilter:
		msg.Payload.Config.PostgresChanges = []postgresFilter{filter.(postgresFilter)}
		break
	case broadcastFilter:
		msg.Payload.Config.Broadcast.Self = true
		break
	case presenceFilter:
		msg.Payload.Config.Presence.Key = ""
		break
	}
	return msg
}
