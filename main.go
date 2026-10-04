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
	"PTTL":    {1, 1},
	"PERSIST": {1, 1},
	"WAIT":    {1, 1},
	"EXISTS":  {1, 1},
}

var store = map[string]*StoreValue{}

var clockOffsetMs int64 = 0

func nowMs() int64 {
	return time.Now().UnixNano()/int64(time.Millisecond) + clockOffsetMs
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
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
		storeValue, ok := store[args[1]]
		if !ok {
			return encodeNullBulkString()
		}
		if expireTTL(storeValue) {
			delete(store, args[1])
			return encodeNullBulkString()
		}
		return encodeBulkString(storeValue.value)

	case "DBSIZE":
		size := 0
		for key, val := range store {
			if expireTTL(val) {
				delete(store, key)
				continue
			}
			size++
		}
		return encodeNumber(size)

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

	case "WAIT":
		ms, err := strconv.Atoi(args[1])
		if err != nil {
			return encodeError("value is not an integer or out of range")
		}
		clockOffsetMs += int64(ms)
		return encodeSimpleString("OK")

	case "EXISTS":
		key := args[1]
		storeValue, ok := store[key]
		if !ok {
			return encodeNumber(0)
		}

		if expireTTL(storeValue) {
			delete(store, args[1])
			return encodeNumber(0)
		}

		return encodeNumber(1)
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

func set(args []string) string {
	var nx, xx bool
	var ttl *int64 = nil

	for idx, val := range args {
		upper := strings.ToUpper(val)
		switch upper {
		case "NX":
			nx = true
		case "XX":
			xx = true
		case "EX":
			if idx+1 >= len(args) {
				return encodeError("syntax error")
			}
			n, err := strconv.Atoi(args[idx+1])
			if err != nil {
				return encodeError("value is not an integer or out of range")
			}
			exp := nowMs() + int64(n)*1000
			ttl = &exp
		case "PX":
			if idx+1 >= len(args) {
				return encodeError("syntax error")
			}
			n, err := strconv.Atoi(args[idx+1])
			if err != nil {
				return encodeError("value is not an integer or out of range")
			}
			exp := nowMs() + int64(n)
			ttl = &exp
		}
	}

	key := args[1]
	value := args[2]

	if nx {
		if _, ok := store[key]; ok {
			return encodeNullBulkString()
		}
	}
	if xx {
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

	prev, ok := store[key]
	if ok && expireTTL(prev) {
		delete(store, key)
		ok = false
	}

	if !ok {
		store[key] = newStoreValue(strconv.Itoa(val))
		return encodeNumber(val)
	}

	prevAsInt, err := strconv.Atoi(prev.value)
	if err != nil {
		return encodeError("value is not an integer or out of range")
	}

	resultValue := prevAsInt + val
	store[key] = newStoreValue(strconv.Itoa(resultValue))
	return encodeNumber(resultValue)
}

func decrement(key string, amount string) string {
	val, err := strconv.Atoi(amount)
	if err != nil {
		return encodeError("value is not an integer or out of range")
	}

	prev, ok := store[key]
	if ok && expireTTL(prev) {
		delete(store, key)
		ok = false
	}

	if !ok {
		result := -val
		store[key] = newStoreValue(strconv.Itoa(result))
		return encodeNumber(result)
	}

	prevAsInt, err := strconv.Atoi(prev.value)
	if err != nil {
		return encodeError("value is not an integer or out of range")
	}

	resultValue := prevAsInt - val
	store[key] = newStoreValue(strconv.Itoa(resultValue))
	return encodeNumber(resultValue)
}

func expire(key string, second string) string {
	secondAsInt, err := strconv.Atoi(second)
	if err != nil {
		return encodeError("value is not an integer or out of range")
	}

	cur, ok := store[key]
	if !ok || cur == nil {
		return encodeNumber(0)
	}

	if cur.TTL != nil && *cur.TTL <= nowMs() {
		delete(store, key)
		return encodeNumber(0)
	}

	actualTTL := nowMs() + int64(secondAsInt)*1000
	cur.TTL = &actualTTL
	return encodeNumber(1)
}

func ttl(key string, unit time.Duration) string {
	cur, ok := store[key]
	if !ok || cur == nil {
		return encodeNumber(-2)
	}

	if cur.TTL == nil {
		return encodeNumber(-1)
	}

	diff := *cur.TTL - nowMs()
	if diff < 0 {
		delete(store, key)
		return encodeNumber(-2)
	}

	if unit == time.Second {
		return encodeNumber(int((diff + 999) / 1000))
	}
	return encodeNumber(int(diff))
}

func persist(key string) string {
	cur, ok := store[key]
	if !ok || cur == nil {
		return encodeNumber(0)
	}

	if cur.TTL == nil {
		return encodeNumber(0)
	}

	if *cur.TTL-nowMs() < 0 {
		delete(store, key)
		return encodeNumber(0)
	}

	cur.TTL = nil
	return encodeNumber(1)
}

func expireTTL(val *StoreValue) bool {
	return val.TTL != nil && *val.TTL-nowMs() < 0
}

// --- RESP encoders ---

func encodeSimpleString(s string) string {
	return "+" + s + "\r\n"
}

func encodeError(msg string) string {
	return "-ERR " + msg + "\r\n"
}

func encodeBulkString(s string) string {
	return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s)
}

func encodeNullBulkString() string {
	return "$-1\r\n"
}

func encodeNumber(num int) string {
	return ":" + strconv.Itoa(num) + "\r\n"
}
