package main

import (
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
	"unicode"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type Client struct {
	conn       *websocket.Conn
	isReadOnly bool
}

type Session struct {
	id          string
	CreatedAt   time.Time
	ptmx        *os.File
	cmd         *exec.Cmd
	clients     map[*websocket.Conn]*Client
	CommandLog  []string // Changed to store final command strings
	inputBuffer []byte   // Buffer to build the current line of input
	mu          sync.RWMutex
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

var manager = SessionManager{
	sessions: make(map[string]*Session),
}

// getSession safely retrieves a session by its ID.
func (sm *SessionManager) getSession(id string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	session, exists := sm.sessions[id]
	return session, exists
}

func (sm *SessionManager) getOrCreateSession(id string, command []string) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[id]; exists {
		return session, nil
	}

	log.Printf("Creating new session: %s", id)
	cmd := exec.Command(command[0], command[1:]...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	session := &Session{
		id:          id,
		CreatedAt:   time.Now(),
		ptmx:        ptmx,
		clients:     make(map[*websocket.Conn]*Client),
		CommandLog:  make([]string, 0), // Initialize as a string slice
		inputBuffer: make([]byte, 0),   // Initialize the input buffer
	}
	sm.sessions[id] = session

	go session.run()

	return session, nil
}

func (s *Session) run() {
	defer func() {
		manager.mu.Lock()
		delete(manager.sessions, s.id)
		manager.mu.Unlock()
		s.ptmx.Close()
		log.Printf("Session %s closed and cleaned up.", s.id)
	}()

	buffer := make([]byte, 1024)
	for {
		n, err := s.ptmx.Read(buffer)
		if err != nil {
			log.Printf("PTY for session %s closed: %v", s.id, err)
			s.mu.RLock()
			for client := range s.clients {
				client.Close()
			}
			s.mu.RUnlock()
			return
		}
		s.broadcast(buffer[:n])
	}
}

func (s *Session) broadcast(message []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.clients {
		if err := client.WriteMessage(websocket.BinaryMessage, message); err != nil {
			log.Printf("Broadcast error (client will be removed): %v", err)
		}
	}
}

// processAndLogInput intelligently processes terminal input to reconstruct commands.
func (s *Session) processAndLogInput(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, charByte := range data {
		char := rune(charByte)
		switch char {
		case '\r': // Carriage return (Enter key)
			if len(s.inputBuffer) > 0 {
				command := string(s.inputBuffer)
				s.CommandLog = append(s.CommandLog, command)
				log.Printf("Session [%s]: Logged command: \"%s\". Total commands: %d", s.id, command, len(s.CommandLog))
				s.inputBuffer = make([]byte, 0) // Clear the buffer
			}
		case '\u007f', '\b': // Backspace or Delete
			if len(s.inputBuffer) > 0 {
				s.inputBuffer = s.inputBuffer[:len(s.inputBuffer)-1]
			}
		default:
			// Append only printable characters, ignore control sequences
			if unicode.IsPrint(char) {
				s.inputBuffer = append(s.inputBuffer, byte(char))
			}
		}
	}
}

// getLog retrieves a copy of the command log.
func (s *Session) getLog() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy to ensure thread safety
	logCopy := make([]string, len(s.CommandLog))
	copy(logCopy, s.CommandLog)
	return logCopy
}

func (s *Session) addClient(conn *websocket.Conn, isReadOnly bool) {
	s.mu.Lock()
	s.clients[conn] = &Client{
		conn:       conn,
		isReadOnly: isReadOnly,
	}
	s.mu.Unlock()
	log.Printf("Client added to session %s. Total clients: %d", s.id, len(s.clients))
}

func (s *Session) removeClient(conn *websocket.Conn) {
	s.mu.Lock()
	delete(s.clients, conn)
	s.mu.Unlock()
	log.Printf("Client removed from session %s. Total clients: %d", s.id, len(s.clients))
	conn.Close()
}
