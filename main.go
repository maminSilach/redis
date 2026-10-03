package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	// "strconv"
)

func handleCommand(args []string) string {
	cmd := strings.ToUpper(args[0])

	switch cmd {
	case "PING":
		if len(args) > 1 {
			return decodeArray(args)
		} else {
			return decodeSingle("PONG")
		}
	case "ECHO":
		return decodeArray(args)
	case "COMMAND":
		if args[1] == "DOCS" {
			return decodeSingle("OK")
		} else {
			return decodeError(fmt.Sprintf("unknown command '%s'", args[1]))
		}

	}

	return decodeError(fmt.Sprintf("unknown command '%s'", cmd))
}

func decodeArray(args []string) string {
	msg := strings.Join(args[1:], " ")

	return fmt.Sprintf("$%d\r\n%s\r\n", len(msg), msg)
}

// +<string>\r\n
func decodeSingle(line string) string {
	return fmt.Sprintf("+%s\r\n", line)
}

// -<error>\r\n
func decodeError(line string) string {
	return fmt.Sprintf("-ERR %s\r\n", line)
}

// // :<number>\r\n
// func decodeNumber(line string) string {

// 	return fmt.Sprintf("-'%s'\r\n", line)
// }

// // $-1\r\n
// func decodeNull() string {
// 	return "$-1\r\n"
// }

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {

		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			continue
		}

		args := parseArgs(line)
		response := handleCommand(args)
		fmt.Print(response)
	}
}

func parseArgs(line string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false

	for _, ch := range line {
		switch {
		case ch == '"' && !inQuotes:
			inQuotes = true
		case ch == '"' && inQuotes:
			inQuotes = false
		case ch == ' ' && !inQuotes:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}
