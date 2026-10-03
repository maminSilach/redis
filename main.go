package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func handleCommand(args []string) string {
	cmd := strings.ToUpper(args[0])

	switch cmd {
	case "PING":
		if len(args) > 1 {
			return decode(args)
		} else {
			return "+PONG\r\n"
		}
	case "ECHO":
		return decode(args)
	}

	return fmt.Sprintf("-ERR unknown command '%s'\r\n", cmd)
}

func decode(args []string) string {
	msg := strings.Join(args[1:], " ")

	return fmt.Sprintf("$%d\r\n%s\r\n", len(msg), msg)
}

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
