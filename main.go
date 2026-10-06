package main

import (
	"bufio"
	"container/list"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type ValueKind int

const (
	KindString ValueKind = iota
	KindList
	KindHash
)

type StoreValue struct {
	value string
	list  *list.List
	TTL   *int64
}

func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}

func newStoreValue(value string) *StoreValue {
	return &StoreValue{
		value: value,
		TTL:   nil,
	}
}

func newStoreValueWithList(list *list.List) *StoreValue {
	return &StoreValue{
		list: list,
		TTL:  nil,
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
	"LPUSH":   {2, 9223372036854775807},
	"RPUSH":   {2, 9223372036854775807},
	"LRANGE":  {2, 3},
	"LPOP":    {1, 1},
	"RPOP":    {1, 1},
	"LLEN":    {1, 1},
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
		res, ok := get(args[1])
		if !ok {
			return encodeNullBulkString()
		} else {
			return encodeBulkString(res.value)
		}

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

	case "RPUSH":
		return push(args, true)
	case "LPUSH":
		return push(args, false)
	case "LRANGE":
		return lrange(args[1], mustAtoi(args[2]), mustAtoi(args[3]))
	case "LPOP":
		return pop(args[1], false)
	case "RPOP":
		return pop(args[1], true)
	case "LLEN":
		return llen(args[1])
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

func get(key string) (*StoreValue, bool) {
	storeValue, ok := store[key]
	if !ok {
		return nil, false
	}
	if expireTTL(storeValue) {
		delete(store, key)
		return nil, false
	}

	return storeValue, true
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

func push(args []string, rpush bool) string {
	key := args[1]
	cur, ok := store[key]
	var listr *list.List

	if !ok || cur == nil {
		listr = list.New()
		val := newStoreValueWithList(listr)
		store[key] = val
	} else {
		if cur.list == nil && cur.value != "" {
			return encodeWrong("Operation against a key holding the wrong kind of value")
		}

		listr = cur.list
	}

	for i := 2; i < len(args); i++ {
		if rpush {
			listr.PushBack(args[i])
		} else {
			listr.PushFront(args[i])
		}
	}

	return encodeNumber(listr.Len())
}

func lrange(key string, start int, end int) string {
	cur, ok := store[key]
	if !ok || cur == nil {
		return encodeEmpty()
	}

	if cur.list == nil {
		return encodeWrong("Operation against a key holding the wrong kind of value")
	}

	return encodeArray(cur.list, start, end)
}

func pop(key string, rpop bool) string {
	cur, ok := get(key)
	if !ok || cur == nil || cur.list == nil {
		return encodeNullBulkString()
	}

	var digit *list.Element
	if rpop {
		digit = cur.list.Back()
		cur.list.Remove(digit)
	} else {
		digit = cur.list.Front()
		cur.list.Remove(digit)
	}

	if cur.list.Len() == 0 {
		delete(store, key)
	}

	return encodeBulkString(digit.Value.(string))
}

func llen(key string) string {
	cur, ok := get(key)
	if !ok || cur == nil || cur.list == nil {
		return encodeNullBulkString()
	}

	return encodeNumber(cur.list.Len())
}

// --- RESP encoders ---

func encodeSimpleString(s string) string {
	return "+" + s + "\r\n"
}

func encodeError(msg string) string {
	return "-ERR " + msg + "\r\n"
}

func encodeWrong(msg string) string {
	return "-WRONGTYPE " + msg + "\r\n"
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

func encodeEmpty() string {
	return "*0\r\n"
}

func encodeArray(items *list.List, start int, end int) string {
	actualStart, actualEnd, ok := normalize(start, end, items.Len())
	if !ok {
		return encodeEmpty()
	}

	var selected []string
	counter := 0
	for e := items.Front(); e != nil && counter <= actualEnd; e = e.Next() {
		if counter >= actualStart {
			selected = append(selected, e.Value.(string))
		}
		counter++
	}

	var sb strings.Builder
	sb.WriteString("*")
	sb.WriteString(strconv.Itoa(len(selected)))
	sb.WriteString("\r\n")

	for _, e := range selected {
		sb.WriteString("$")
		sb.WriteString(strconv.Itoa(len(e)))
		sb.WriteString("\r\n")
		sb.WriteString(e)
		sb.WriteString("\r\n")
	}

	return sb.String()
}

func normalize(start, end, length int) (int, int, bool) {
	if start < 0 {
		start = length + start
		if start < 0 {
			start = 0
		}
	}

	if end < 0 {
		end = length + end
	}

	if end >= length {
		end = length - 1
	}

	if start > end || start >= length {
		return 0, 0, false
	}

	return start, end, true
}
