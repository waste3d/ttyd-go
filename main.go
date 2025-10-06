// main.go
package main

import (
	"embed"
	"log"
	"net/http"
	"os"

	"github.com/urfave/cli/v2"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	var credential string

	app := &cli.App{
		Name:  "go-ttyd",
		Usage: "Share your terminal over the web, with auth and session sharing",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "port", Value: "8080", Usage: "Port to listen on", Aliases: []string{"p"}},
			&cli.StringFlag{Name: "credential", Value: "", Usage: "Credential for basic auth (format: user:pass)", Aliases: []string{"c"}, Destination: &credential},
		},
		Action: func(c *cli.Context) error {
			port := c.String("port")
			command := c.Args().Slice()
			if c.NArg() == 0 {
				command = []string{"bash"}
			}

			// Настраиваем все обработчики HTTP.
			handler := registerHandlers(command, credential)

			log.Printf("Starting go-ttyd on port %s...", port)
			if credential != "" {
				log.Println("Authentication is ENABLED.")
			}
			log.Println("Server started. Open http://localhost:" + port + " in your browser.")

			// Запускаем сервер с настроенными обработчиками.
			if err := http.ListenAndServe(":"+port, handler); err != nil {
				log.Fatal("ListenAndServe:", err)
			}
			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
