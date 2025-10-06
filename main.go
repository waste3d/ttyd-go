package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/urfave/cli/v2"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, command []string) {
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

func main() {
	app := &cli.App{
		Name:  "go-ttyd",
		Usage: "A simple command-line tool to share your terminal over the web",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "port",
				Value:   "8080",
				Usage:   "Port to listen on",
				Aliases: []string{"p"},
			},
		},
		Action: func(c *cli.Context) error {
			port := c.String("port")
			command := c.Args().Slice()
			if c.NArg() == 0 {
				command = []string{"bash"}
			}
			log.Printf("Starting go-ttyd on port %s...", port)
			log.Printf("Command to execute: %v", command)

			fs := http.FileServer(http.Dir("./static"))
			http.Handle("/", fs)

			http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
				handleWebSocket(w, r, command)
			})

			log.Println("Server started. Open http://localhost:" + port + " in your browser.")

			if err := http.ListenAndServe(":"+port, nil); err != nil {
				log.Fatal("ListenAndServe:", err)
			}
			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
