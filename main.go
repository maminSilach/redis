package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type StoreValue struct {
	value string
	TTL   *int64
}

func newStoreValue(value string) *StoreValue {
	return &StoreValue{
		value: value,
		TTL:   nil,
	}
}

func newStoreValueWithTTL(value string, TTL *int64) *StoreValue {
	return &StoreValue{
		value: value,
		TTL:   TTL,
	}
}

var arity = map[string][2]int{
	"PING":    {0, 1},
	"ECHO":    {1, 1},
	"SET":     {2, 5},
	"GET":     {1, 1},
	"DBSIZE":  {0, 0},
	"INCR":    {1, 1},
	"DECR":    {1, 1},
	"INCRBY":  {2, 2},
	"DECRBY":  {2, 2},
	"EXPIRE":  {2, 2},
	"TTL":     {1, 1},
	"PERSIST": {1, 1},
	"PTTL":    {1, 1},
}

var store = map[string]*StoreValue{}

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
		return set(args)
	case "GET":
		if storeValue, ok := store[args[1]]; ok {
			return encodeBulkString(storeValue.value)
		}
		return encodeNullBulkString()
	case "DBSIZE":
		return encodeNumber(dbSize())
	case "INCR":
		return increment(args[1], "1")
	case "DECR":
		return decrement(args[1], "1")
	case "INCRBY":
		return increment(args[1], args[2])
	case "DECRBY":
		return decrement(args[1], args[2])
	case "EXPIRE":
		return expire(args[1], args[2])
	case "TTL":
		return ttl(args[1], time.Second)
	case "PTTL":
		return ttl(args[1], time.Millisecond)
	case "PERSIST":
		return persist(args[1])
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

func set(args []string) string {
	var nx, xx bool
	var ttl *int64 = nil

	for idx, val := range args {
		if val == "NX" {
			nx = true
		} else if val == "XX" {
			xx = true
		} else if val == "EX" {
			val, err := strconv.Atoi(args[idx+1])
			if err != nil {
				return encodeError("value is not an integer or out of range")
			}

			exp := time.Now().UnixNano()/int64(time.Millisecond) + int64(val)*1000
			ttl = &exp
		} else if val == "PX" {
			val, err := strconv.Atoi(args[idx+1])
			if err != nil {
				return encodeError("value is not an integer or out of range")
			}

			exp := time.Now().UnixNano()/int64(time.Millisecond) + int64(val)
			ttl = &exp
		}
	}

	key := args[1]
	value := args[2]
	if nx {
		if _, ok := store[key]; ok {
			return encodeNullBulkString()
		}
	} else if xx {
		if _, ok := store[key]; !ok {
			return encodeNullBulkString()
		}
	}

	store[key] = newStoreValueWithTTL(value, ttl)
	return encodeSimpleString("OK")
}

func increment(key string, amount string) string {
	val, err := strconv.Atoi(amount)
	if err != nil {
		return encodeError("value is not an integer or out of range")
	}

	var resultValue = val
	prev, ok := store[key]
	if !ok {
		store[key] = newStoreValue(amount)
	} else {
		prevAsInt, err := strconv.Atoi(prev.value)
		if err != nil {
			return encodeError("value is not an integer or out of range")
		}

		resultValue = prevAsInt + val
		store[key] = newStoreValue(strconv.Itoa(resultValue))
		return encodeNumber(resultValue)
	}

	return encodeNumber(resultValue)
}

func decrement(key string, amount string) string {
	val, err := strconv.Atoi(amount)
	if err != nil {
		return encodeError("value is not an integer or out of range")
	}

	var resultValue = val
	prev, ok := store[key]
	if !ok {
		store[key] = newStoreValue(strconv.Itoa(-resultValue))
	} else {
		prevAsInt, err := strconv.Atoi(prev.value)
		if err != nil {
			return encodeError("value is not an integer or out of range")
		}

		resultValue = prevAsInt - val
		store[key] = newStoreValue(strconv.Itoa(resultValue))
		return encodeNumber(resultValue)
	}

	return encodeNumber(resultValue)
}

func expire(key string, second string) string {
	secondAsInt, err := strconv.Atoi(second)
	if err != nil {
		return encodeError("value is not an integer or out of range")
	}

	cur, ok := store[key]
	if !ok {
		return encodeNumber(0)
	}

	actualTTL := time.Now().UnixNano()/int64(time.Millisecond) + int64(secondAsInt)*1000
	cur.TTL = &actualTTL

	return encodeNumber(1)
}

func ttl(key string, unit time.Duration) string {
	cur, ok := store[key]
	if !ok {
		return encodeNumber(-2)
	}

	if cur.TTL == nil {
		return encodeNumber(-1)
	}

	diff := *cur.TTL - time.Now().UnixNano()/int64(time.Millisecond)
	if diff < 0 {
		return encodeNumber(-2)
	}

	if unit == time.Second {
		return encodeNumber(int(diff / 1000))
	} else {
		return encodeNumber(int(diff))
	}
}

func persist(key string) string {
	cur, ok := store[key]
	if !ok || cur.TTL == nil || *cur.TTL-time.Now().UnixNano()/int64(time.Millisecond) < 0 {
		return encodeNumber(0)
	}

	cur.TTL = nil
	return encodeNumber(1)
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
