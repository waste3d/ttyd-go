// handlers.go
package main

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func websocketHandler(w http.ResponseWriter, r *http.Request, command []string, isReadOnly bool) {
	var sessionID string
	if strings.HasPrefix(r.URL.Path, "/ws-ro/") {
		sessionID = strings.TrimPrefix(r.URL.Path, "/ws-ro/")
	} else {
		sessionID = strings.TrimPrefix(r.URL.Path, "/ws/")
	}

	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	session, err := manager.getOrCreateSession(sessionID, command)
	if err != nil {
		log.Printf("Failed to get or create session %s: %v", sessionID, err)
		http.Error(w, "Failed to start session", http.StatusInternalServerError)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade websocket: %v", err)
		return
	}
	session.addClient(conn)
	defer session.removeClient(conn)

	log.Printf("Client connected to session %s (read-only: %v)", sessionID, isReadOnly)

	if !isReadOnly {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var resizeMessage struct {
				Type string `json:"type"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if json.Unmarshal(message, &resizeMessage) == nil && resizeMessage.Type == "resize" {
				pty.Setsize(session.ptmx, &pty.Winsize{
					Rows: resizeMessage.Rows,
					Cols: resizeMessage.Cols,
				})
			} else {
				session.ptmx.Write(message)
			}
		}
	} else {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}
}

func authMiddleware(next http.Handler, credential string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if credential == "" {
			next.ServeHTTP(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || credential != (user+":"+pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func registerHandlers(command []string, credential string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/ws/", func(w http.ResponseWriter, r *http.Request) {
		websocketHandler(w, r, command, false)
	})

	mux.HandleFunc("/ws-ro/", func(w http.ResponseWriter, r *http.Request) {
		websocketHandler(w, r, command, true)
	})

	subFS, _ := fs.Sub(staticFiles, "static")
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	return authMiddleware(mux, credential)
}
