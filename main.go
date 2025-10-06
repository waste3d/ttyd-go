package main

import (
	"io"
	"log"
	"net/http"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleWebsocket(w http.ResponseWriter, r *http.Request, command []string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrader error:", err)
	}
	defer conn.Close()

	cmd := exec.Command(command[0], command[1:]...)

	// Запускаем команду в псевдотерминале (PTY)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Println("Failed to start pty:", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Error starting command."))
		return
	}
	defer ptmx.Close()

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		wsWriter := &wsWrapper{
			conn: conn,
		}
		if _, err := io.Copy(wsWriter, ptmx); err != nil {
			log.Println("Error copying from pty to websocket:", err)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Println("Error reading from websocket:", err)
				break
			}

			if _, err := ptmx.Write(message); err != nil {
				log.Println("Error writing to pty:", err)
				break
			}
		}
	}()

	wg.Wait()
	log.Println("Client disconnected.")
}

type wsWrapper struct {
	conn *websocket.Conn
}

func (w *wsWrapper) Write(p []byte) (n int, err error) {
	err = w.conn.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
