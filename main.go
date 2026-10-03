package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var arity = map[string][2]int{
	"PING":   {0, 1},
	"ECHO":   {1, 1},
	"SET":    {2, 2},
	"GET":    {1, 1},
	"DBSIZE": {0, 0},
}

var store = map[string]string{}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		args := parseArgs(line)
		if len(args) == 0 {
			continue
		}

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

func handleCommand(args []string) string {
	if len(args) == 0 {
		return encodeError("empty command")
	}

	cmd := strings.ToUpper(args[0])
	argCount := len(args) - 1

	if message, err := checkArity(cmd, argCount); err != nil {
		return message
	}

	switch cmd {
	case "PING":
		if len(args) > 1 {
			return encodeBulkString(args[1])
		}
		return encodeSimpleString("PONG")

	case "ECHO":
		return encodeBulkString(args[1])

	case "SET":
		store[args[1]] = args[2]
		return encodeSimpleString("OK")

	case "GET":
		if val, ok := store[args[1]]; ok {
			return encodeBulkString(val)
		}
		return encodeNullBulkString()
	case "DBSIZE":
		return encodeNumber(dbSize())
	}

	return encodeError(fmt.Sprintf("unknown command '%s'", cmd))
}

func checkArity(cmd string, argCount int) (string, error) {
	bounds, ok := arity[cmd]
	if ok && (argCount < bounds[0] || argCount > bounds[1]) {
		msg := fmt.Sprintf("wrong number of arguments for '%s' command", cmd)
		return encodeError(msg), errors.New(msg)
	}
	return "", nil
}

func dbSize() int {
	return len(store)
}

// --- RESP encoders ---

// +OK\r\n
func encodeSimpleString(s string) string {
	return "+" + s + "\r\n"
}

// -ERR message\r\n
func encodeError(msg string) string {
	return "-ERR " + msg + "\r\n"
}

// $len\r\n<bytes>\r\n
func encodeBulkString(s string) string {
	return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s)
}

// $-1\r\n
func encodeNullBulkString() string {
	return "$-1\r\n"
}

// $:1\r\n
func encodeNumber(num int) string {
	return ":" + strconv.Itoa(num) + "\r\n"
}
