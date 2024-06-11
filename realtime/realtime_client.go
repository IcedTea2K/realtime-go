package realtime

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type RealtimeClient struct {
   Url               string
   ApiKey            string

   mu                sync.Mutex
   conn              *websocket.Conn
   closed            chan struct{}
   logger            *log.Logger
   dialTimeout       time.Duration
   reconnectInterval time.Duration
   heartbeatDuration time.Duration
   heartbeatInterval time.Duration

   bindingQueue         *list.List
   bindingSubscription  map[string]binding
   replyChan            chan string
   currentTopics        map[string][]string
}

type binding struct {
   msg      *ConnectionMsg
   callback func(any)
}

// Create a new RealtimeClient with user's speicfications
func CreateRealtimeClient(projectRef string, apiKey string) *RealtimeClient {
   realtimeUrl := fmt.Sprintf(
      "wss://%s.supabase.co/realtime/v1/websocket?apikey=%s&log_level=info&vsn=1.0.0",
      projectRef,
      apiKey,
   )
   newLogger := log.Default()

   return &RealtimeClient{
      Url: realtimeUrl,
      ApiKey: apiKey,
      logger: newLogger,
      dialTimeout: 10 * time.Second,
      heartbeatDuration: 5   * time.Second,
      heartbeatInterval: 20  * time.Second,
      reconnectInterval: 500 * time.Millisecond,

      currentTopics: make(map[string][]string),
      bindingQueue: list.New(),
   }
}

// Connect the client with the realtime server
func (client *RealtimeClient) Connect() error {
   if client.isClientAlive() {
      return nil
   }

   // Change status of client to alive
   client.closed = make(chan struct{})

   // Attempt to dial the server
   err := client.dialServer()
   if err != nil {
      close(client.closed)
      return fmt.Errorf("Cannot connect to the server: %w", err)
   }

   go client.startHeartbeats()
   go client.startListening()

   return nil
}

// Disconnect the client from the realtime server
func (client *RealtimeClient) Disconnect() error {
   client.mu.Lock()
   defer client.mu.Unlock()

   if !client.isClientAlive() {
      return nil
   }

   err := client.conn.Close(websocket.StatusNormalClosure, "Closing the connection")
   if err != nil {
      if !client.isConnectionAlive(err) {
         client.logger.Println("Connection has already been terminated")
         close(client.closed)
      } else {
         return fmt.Errorf("Failed to close the connection: %w", err)
      }
   } else {
      close(client.closed)
   }
   
   return nil
}

// Begins subscribing to events in the bindingQueue
func (client *RealtimeClient) subscribe() error {
   if !client.isClientAlive() {
      client.Connect()
   }

   if client.replyChan != nil {
      client.replyChan = make(chan string)
   }
   bindNode := client.bindingQueue.Front()

   // TODO: take in a context to allow user to cancel subcribing
   // TODO: add a way to either roll back (unscribe) if an error encounters or 
   //       disconnect to client and close connection
   for bindNode != nil {
      bind := bindNode.Value.(binding)

      err := wsjson.Write(context.Background(), client.conn, bind.msg)
      if err != nil {
         return fmt.Errorf("Unable to subscribe to the event %v: %v", *bind.msg, err)
      }

      select {
         case rep, ok := <- client.replyChan:
            if !ok {
               return fmt.Errorf("Error: Unable to subscribe to the event %v succesfully", bind.msg)
            }
            client.bindingSubscription[rep] = bind
            break
      }
      bindNode = bindNode.Next()
   }

   return nil
}

// Create a new channel with given topic string
func (client *RealtimeClient) Channel(newTopic string) (*RealtimeChannel, error) {
   if _, ok := client.currentTopics[newTopic]; ok {
      return nil, fmt.Errorf("Error: channel with %v topic already created", newTopic)
   }
   newChannel  := CreateRealtimeChannel(client, "realtime:" + newTopic)
   // TODO: Fix currentTopics
   client.currentTopics[newTopic] = []string{}

   return newChannel, nil
}

// Start sending heartbeats to the server to maintain connection
func (client *RealtimeClient) startHeartbeats() {
   for client.isClientAlive() {
      err := client.sendHeartbeat()

      if err != nil {
         if client.isConnectionAlive(err) {
            client.logger.Println(err) 
         } else {
            client.logger.Println("Error: lost connection with the server")
            client.logger.Println("Attempting to to send hearbeat again")

            ctx, cancel := context.WithCancel(context.Background())
            defer cancel()

            // there should never be an error returned, since it'll keep trying
            _ = client.reconnect(ctx)
         }
      }

      // in case where the client needs to reconnect with the server,
      // the interval between heartbeats be however long it takes to
      // reconnect plus the number of heartbeatInterval has gone by
      time.Sleep(client.heartbeatInterval)
   }
}

// Send the heartbeat to the realtime server
func (client *RealtimeClient) sendHeartbeat() error {
   msg := HearbeatMsg{
      TemplateMsg: &TemplateMsg{
         Event: heartbeatEvent,
         Topic: "phoenix",
         Ref: "",
      },
      Payload: struct{}{},
   }

   ctx, cancel := context.WithTimeout(context.Background(), client.heartbeatDuration)
   defer cancel()

   client.logger.Print("Sending heartbeat")

   err := wsjson.Write(ctx, client.conn, msg)
   if err != nil {
      return fmt.Errorf("Failed to send hearbeat in %f seconds: %w", client.heartbeatDuration.Seconds(), err)
   }

   return nil
}

// Keep reading from the connection from the connection
func (client *RealtimeClient) startListening() {
   ctx := context.Background()

   for client.isClientAlive() {
      var msg AbstractMsg

      // Read from the connection
      err := wsjson.Read(ctx, client.conn, &msg)

      // Check if there's a way to partially marshal bytes into an object
      // Or check if polymorphism in go (from TemplateMsg to another type of messg)
      if err != nil {
         if client.isConnectionAlive(err) {
            client.logger.Printf("Unexpected error while listening: %v", err) 
         } else {
            // Quick sleep to prevent taking up CPU cycles.
            // Client should be able to reconnect automatically if it's still alive
            time.Sleep(client.reconnectInterval)
         }
      } else {
         // Spawn a new thread to process the server's respond
         go client.processMessage(msg) 
      }
   }
}

// Process the given message according certain events
func (client *RealtimeClient) processMessage(msg AbstractMsg) {
   client.logger.Printf("Header: %+v", *msg.TemplateMsg)
   client.logger.Printf("Payload: %+v", string(msg.Payload))
   genericPayload, err := client.unmarshalPayload(msg)
   if err != nil {
      client.logger.Printf("Unable to process received message: %v", err)
      client.logger.Printf("%v", genericPayload)
      return
   }

   switch payload := genericPayload.(type) {
      case *ReplyPayload:
         client.logger.Printf("Processing: %+v", payload)
         break
   }
}

func (client *RealtimeClient) unmarshalPayload(msg AbstractMsg) (any, error) {
   var payload any
   var err error

   // Parse the payload depending on the event type
   switch msg.Event {
      case replyEvent: 
         payload = new(ReplyPayload)
         break
      case postgresChangesEvent:
         break
      case systemEvent:
         payload = new(SystemPayload)
         break
      case presenceStateEvent:
         payload = new(PresenceStatePayload)
         break
      default:
         return struct{}{}, fmt.Errorf("Error: Unsupported event %v", msg.Event)
   }

   err = json.Unmarshal(msg.Payload, payload) 
   if err != nil {
      return struct{}{}, fmt.Errorf("Error: Unable to unmarshal payload: %v", err)
   }
   return payload, nil
}

// Dial the server with a certain timeout in seconds
func (client *RealtimeClient) dialServer() error {
   client.mu.Lock()
   defer client.mu.Unlock()

   if !client.isClientAlive() {
      return nil
   }

   ctx, cancel := context.WithTimeout(context.Background(), client.dialTimeout)
   defer cancel()

   conn, _, err := websocket.Dial(ctx, client.Url, nil)
   if err != nil {
      return fmt.Errorf("Failed to dial the server: %w", err)
   }

   client.conn = conn

   return nil
}

// Keep trying to reconnect every 0.5 seconds until ctx is done/invalidated
func (client *RealtimeClient) reconnect(ctx context.Context) error {
   for client.isClientAlive() {
      client.logger.Println("Attempt to reconnect to the server")

      select {
         case <-ctx.Done():
            return fmt.Errorf("Failed to reconnect to the server within time limit")
         default:
            err := client.dialServer()
            if err == nil {
               return nil
            }

            client.logger.Printf("Failed to reconnect to the server: %s", err)
            time.Sleep(client.reconnectInterval)
      }
   }

   return nil
}

// Check if the realtime client has been killed
func (client *RealtimeClient) isClientAlive() bool {
   if client.closed == nil {
      return false
   }

   select {
      case <-client.closed:
         return false
      default:
         break
   }

   return true
}

// Add event bindings to the bindingQueue
func (client *RealtimeClient) addBinding(newBinding binding) {
   client.bindingQueue.PushBack(newBinding)
}

// The underlying package of websocket returns an error if the connection is
// terminated on the server side. Therefore, the state of the connection can 
// be achieved by investigating the error
// Constraints: err must be returned from interacting with the connection
func (client *RealtimeClient) isConnectionAlive(err error) bool {
   return !errors.Is(err, io.EOF)
}
