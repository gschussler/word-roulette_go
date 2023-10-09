// main file of execution -- package main at the top of other packages removes requirement to import here
// http server and WebSocket initialization

package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var lobbyConnections = make(map[string][]*websocket.Conn)

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// upgrade http connection to a WebSocket connection using upgrader struct
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading to WebSocket: ", err)
		return
	}
	defer conn.Close()

	// import LobbyInfo struct from models.go
	var lobbyInfo LobbyInfo

	err = conn.ReadJSON(&lobbyInfo)
	if err != nil {
		log.Println("Error reading lobby information", err)
		deleteEmptyLobbies(lobbyInfo.Lobby)
		return
	}

	// used often in following code, assigned to variable
	lobby := lobbyInfo.Lobby

	// check if lobby name exists in lobbyConnections
	if _, exists := lobbyConnections[lobby]; !exists {
		lobbyConnections[lobby] = make([]*websocket.Conn, 0)
	}

	switch lobbyInfo.Action {
	case "join":
		// add the connection to the lobby's list of clients
		lobbyConnections[lobby] = append(lobbyConnections[lobby], conn)
		log.Printf(`"%s" connected to Lobby "%s" -- Socket opened`, lobbyInfo.User, lobby)

		// retrieve existing messages from Redis
		existingMessages := getExistingMessages(lobby)
		for _, message := range existingMessages {
			// send each message to the connected client
			msgJSON, err := json.Marshal(message)
			if err != nil {
				log.Printf("Error serializing existing message: %v", err)
				continue
			}
			conn.WriteMessage(websocket.TextMessage, msgJSON)
		}
	default:
		log.Printf("Unknown action: %s", lobbyInfo.Action)
	}

	for {
		// Read a message from the WebSocket
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// log.Println("Error reading message: ", err)
			// remove connection from the lobby
			removeUserFromLobby(lobby, conn)
			log.Printf(`"%s" disconnected from Lobby "%s" -- Socket closed`, lobbyInfo.User, lobby)

			// check if lobby is empty in order to delete messages from Redis
			if len(lobbyConnections[lobby]) == 0 {
				deleteEmptyLobbies(lobby)
			}
			return
		}

		var ReceivedMessage struct {
			Lobby   string `json:"lobby"`
			User    string `json:"user"`
			Content string `json:"content"`
		}

		if err := json.Unmarshal(msg, &ReceivedMessage); err != nil {
			log.Printf("Error unmarshaling sent message content: %v", err)
			return
		}

		log.Printf(`msg is -- %s`, ReceivedMessage.Content)

		// build message from struct to be stored in Redis
		message := Message{
			ID:      generateMessageID(),
			Lobby:   lobby,
			User:    lobbyInfo.User,
			Content: ReceivedMessage.Content,
			Time:    time.Now(),
		}

		message.FormattedTime = message.Time.Format("03:04 PM")

		storeMessage(message)

		broadcastMessage(lobby, message)
	}

	// 	// Optionally, you can send a response back to the client
	// 	err = conn.WriteMessage(websocket.TextMessage, []byte("message sent"))
	// 	if err != nil {
	// 		log.Println("Error writing response: ", err)
	// 		break
	// 	}
	// }
}

func generateMessageID() string {
	id := uuid.New()
	return id.String()
}

func removeUserFromLobby(lobby string, conn *websocket.Conn) {
	// remove a connection from the list of clients in the specified lobby
	connections := lobbyConnections[lobby]
	for i, c := range connections {
		if c == conn {
			lobbyConnections[lobby] = append(connections[:i], connections[i+1:]...)
			break
		}
	}
}

func broadcastMessage(lobby string, message Message) {
	// serialize message to JSON
	msgJSON, err := json.Marshal(message)
	if err != nil {
		log.Println("Error serializing message to JSON: ", err)
		return
	}
	// broadcast a message to all clients in the specified lobby
	connections := lobbyConnections[lobby]
	for _, conn := range connections {
		err := conn.WriteMessage(websocket.TextMessage, msgJSON)
		if err != nil {
			log.Println("Error writing message: ", err)
		}
	}
}

// // extract lobby name info from user request to enter lobby
// func getLobbyName(r *http.Request) string {

// }

func main() {
	// init Redis db
	initRedis()

	// start http server for homepage
	// prepare WebSocket for incoming connections
	log.Println("server started on port 8085")
	http.Handle("/", http.FileServer(http.Dir("../frontend/dist")))
	http.HandleFunc("/ws", handleWebSocket)

	err := http.ListenAndServe(":8085", nil)
	if err != nil {
		log.Fatal("Error starting server: ", err)
	}
}
