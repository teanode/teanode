//go:build linux || darwin

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/screenbuffer"
	"github.com/urfave/cli/v3"
)

const (
	defaultTerminalRows uint16 = 24
	defaultTerminalCols uint16 = 80
)

func NewTerminalCommand() *cli.Command {
	return &cli.Command{
		Name:      "terminal",
		Usage:     "Launch a PTY-backed terminal and relay it to the node",
		ArgsUsage: "[-- command [arguments...]]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "node",
				Usage: "node WebSocket URL (e.g. ws://127.0.0.1:8833)",
				Value: "ws://127.0.0.1:8833",
			},
			&cli.StringFlag{
				Name:    "token",
				Aliases: []string{"t"},
				Usage:   "authentication token",
			},
			&cli.StringFlag{
				Name:    "name",
				Aliases: []string{"n"},
				Usage:   "connection name for identifying this terminal",
			},
			&cli.StringFlag{
				Name:    "command",
				Aliases: []string{"c"},
				Usage:   "command to run with arguments (e.g. \"python3 -i\")",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			// Determine child command: --command flag, positional args, $SHELL, or bash.
			var shellArguments []string
			if commandFlag := command.String("command"); commandFlag != "" {
				shellArguments = strings.Fields(commandFlag)
			} else {
				shellArguments = command.Args().Slice()
			}
			if len(shellArguments) == 0 {
				if shell := os.Getenv("SHELL"); shell != "" {
					shellArguments = []string{shell}
				} else {
					shellArguments = []string{"bash"}
				}
			}

			// Open PTY.
			master, slave, err := terminals.OpenPTY()
			if err != nil {
				return fmt.Errorf("open pty: %w", err)
			}
			defer func() { _ = master.Close() }()

			// Set PTY window size from the local terminal. Try stdout/stderr first
			// because stdin may be piped in some invocations.
			rows, cols := currentTerminalSize()
			_ = terminals.SetWinSize(int(master.Fd()), rows, cols)

			// Start child process.
			child := exec.CommandContext(ctx, shellArguments[0], shellArguments[1:]...)
			child.Stdin = slave
			child.Stdout = slave
			child.Stderr = slave
			child.SysProcAttr = &syscall.SysProcAttr{
				Setsid:  true,
				Setctty: true,
				Ctty:    0,
			}
			if err := child.Start(); err != nil {
				_ = slave.Close()
				return fmt.Errorf("start command: %w", err)
			}
			_ = slave.Close()

			// Put user terminal in raw mode.
			originalTermios, err := terminals.MakeRaw(int(os.Stdin.Fd()))
			if err != nil {
				return fmt.Errorf("raw mode: %w", err)
			}
			defer terminals.RestoreTermios(int(os.Stdin.Fd()), originalTermios)

			// Screen buffer for capturing output.
			buffer := screenbuffer.NewWithSize(1000, rows, cols)

			// Goroutine: stdin -> PTY master.
			go func() {
				defer deferutil.Recover()
				_, _ = io.Copy(master, os.Stdin)
			}()

			// Goroutine: PTY master -> stdout + screen buffer.
			go func() {
				defer deferutil.Recover()
				chunk := make([]byte, 4096)
				for {
					bytesRead, err := master.Read(chunk)
					if bytesRead > 0 {
						_, _ = os.Stdout.Write(chunk[:bytesRead])
						buffer.Write(chunk[:bytesRead])
					}
					if err != nil {
						return
					}
				}
			}()

			// Handle SIGWINCH.
			sigwinch := make(chan os.Signal, 1)
			signal.Notify(sigwinch, syscall.SIGWINCH)
			defer signal.Stop(sigwinch)
			go func() {
				defer deferutil.Recover()
				for range sigwinch {
					rows, cols := currentTerminalSize()
					_ = terminals.SetWinSize(int(master.Fd()), rows, cols)
					buffer.Resize(rows, cols)
				}
			}()

			// Connect to node WebSocket.
			nodeUrl := command.String("node")
			token := command.String("token")
			name := command.String("name")
			if name == "" {
				name = defaultTerminalConnectionId()
			}
			shellCommand := strings.Join(shellArguments, " ")
			go connectNode(ctx, nodeUrl, token, name, shellCommand, master, buffer)

			// Wait for child to exit.
			_ = child.Wait()
			return nil
		},
	}
}

func defaultTerminalConnectionId() string {
	username := "teanode"
	if currentUser, err := user.Current(); err == nil {
		if value := currentUser.Username; value != "" {
			username = value
		}
	}
	hostname, _ := os.Hostname()
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = "host"
	}
	return fmt.Sprintf("%s@%s:%d", username, hostname, os.Getpid())
}

func currentTerminalSize() (rows, cols uint16) {
	candidates := []int{int(os.Stdout.Fd()), int(os.Stderr.Fd()), int(os.Stdin.Fd())}
	for _, fileDescriptor := range candidates {
		if rows, cols, err := terminals.GetWinSize(fileDescriptor); err == nil && rows > 0 && cols > 0 {
			return rows, cols
		}
	}
	return defaultTerminalRows, defaultTerminalCols
}

func connectNode(ctx context.Context, nodeUrl, token, name, shellCommand string, master *os.File, buffer *screenbuffer.Buffer) {
	defer deferutil.Recover()
	parsedUrl, err := url.Parse(nodeUrl)
	if err != nil {
		log.Errorf("terminal: invalid node URL: %v", err)
		return
	}
	parsedUrl.Path = "/api/terminal"
	query := parsedUrl.Query()
	if token != "" {
		query.Set("token", token)
	}
	query.Set("id", name)
	parsedUrl.RawQuery = query.Encode()

	for {
		serveNodeConnection(ctx, parsedUrl.String(), shellCommand, master, buffer)

		// Wait before reconnecting, or exit if context is cancelled.
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
			log.Debug("terminal: reconnecting to node ...")
		}
	}
}

func serveNodeConnection(ctx context.Context, url string, shellCommand string, master *os.File, buffer *screenbuffer.Buffer) {
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		log.Errorf("terminal: node connect failed: %v", err)
		return
	}
	defer func() { _ = connection.Close() }()

	log.Debug("terminal: connected to node")

	// Send machine info so the relay can distinguish this terminal.
	sendMachineInfo(connection, shellCommand)

	// Read commands from node.
	for {
		_, data, err := connection.ReadMessage()
		if err != nil {
			log.Warningf("terminal: node connection lost: %v", err)
			return
		}

		var message struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}

		switch message.Method {
		case "ping":
			response, _ := json.Marshal(map[string]string{"method": "pong"})
			_ = connection.WriteMessage(websocket.TextMessage, response)

		case "screenshot":
			text := buffer.Screenshot(100)
			result, _ := json.Marshal(map[string]string{"text": text})
			response, _ := json.Marshal(map[string]interface{}{
				"id":     message.ID,
				"result": json.RawMessage(result),
			})
			_ = connection.WriteMessage(websocket.TextMessage, response)

		case "write":
			var parameters struct {
				Data string `json:"data"`
			}
			if message.Params != nil {
				_ = json.Unmarshal(message.Params, &parameters)
			}
			_, writeErr := master.Write([]byte(parameters.Data))
			if writeErr != nil {
				errorString := writeErr.Error()
				response, _ := json.Marshal(map[string]interface{}{
					"id":    message.ID,
					"error": errorString,
				})
				_ = connection.WriteMessage(websocket.TextMessage, response)
			} else {
				response, _ := json.Marshal(map[string]interface{}{
					"id":     message.ID,
					"result": json.RawMessage("{}"),
				})
				_ = connection.WriteMessage(websocket.TextMessage, response)
			}

		case "resize":
			var parameters struct {
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if message.Params != nil {
				_ = json.Unmarshal(message.Params, &parameters)
			}
			if parameters.Rows > 0 && parameters.Cols > 0 {
				_ = terminals.SetWinSize(int(master.Fd()), parameters.Rows, parameters.Cols)
			}
			response, _ := json.Marshal(map[string]interface{}{
				"id":     message.ID,
				"result": json.RawMessage("{}"),
			})
			_ = connection.WriteMessage(websocket.TextMessage, response)
		}
	}
}

func sendMachineInfo(connection *websocket.Conn, shellCommand string) {
	hostname, _ := os.Hostname()
	username := ""
	if currentUser, err := user.Current(); err == nil {
		username = currentUser.Username
	}
	workingDirectory, _ := os.Getwd()
	timezone := time.Now().Location().String()

	message := map[string]interface{}{
		"method": "attach",
		"params": map[string]string{
			"hostname":         hostname,
			"username":         username,
			"os":               runtime.GOOS,
			"architecture":     runtime.GOARCH,
			"shellCommand":     shellCommand,
			"workingDirectory": workingDirectory,
			"timezone":         timezone,
		},
	}
	data, _ := json.Marshal(message)
	_ = connection.WriteMessage(websocket.TextMessage, data)
}
