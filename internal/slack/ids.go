package slack

import (
	"strconv"
	"strings"
)

// Block IDs and element IDs used by the dynamic flow.
const (
	BlockResourceKey   = "block_resource_key"
	ElemResourceKey    = "elem_resource_key"
	BlockJustification = "block_justification"
	ElemJustification  = "elem_justification"

	blockFieldPrefix = "block_"
	elemFieldPrefix  = "elem_"
	blockMapPrefix   = "block_map_"
	elemMapPrefix    = "elem_map_"
)

func fieldBlockID(path string) string         { return blockFieldPrefix + path }
func fieldElemID(path string) string          { return elemFieldPrefix + path }
func mapEntryBlockID(path, key string) string { return blockMapPrefix + path + "_" + key }
func mapEntryElemID(path, key string) string  { return elemMapPrefix + path + "_" + key }

// Callback IDs.
const (
	CallbackDynamicSelectTarget = "dynamic_select_target"

	callbackDynamicCreatePrefix = "dynamic_create_step_"
	callbackDynamicUpdatePrefix = "dynamic_update_step_"
)

// flowMode tells apart create-new vs edit-existing flows.
type flowMode int

const (
	flowCreate flowMode = iota
	flowUpdate
)

// dynamicCallback is a parsed dynamic_(create|update)_step_N identifier.
// Step is the 1-based step index as it appears in the callback ID.
type dynamicCallback struct {
	Mode flowMode
	Step int
}

func (c dynamicCallback) String() string {
	prefix := callbackDynamicCreatePrefix
	if c.Mode == flowUpdate {
		prefix = callbackDynamicUpdatePrefix
	}
	return prefix + strconv.Itoa(c.Step)
}

// parseDynamicCallback returns the parsed callback or (zero, false) when id is
// not a dynamic step callback.
func parseDynamicCallback(id string) (dynamicCallback, bool) {
	mode := flowCreate
	rest, ok := strings.CutPrefix(id, callbackDynamicCreatePrefix)
	if !ok {
		rest, ok = strings.CutPrefix(id, callbackDynamicUpdatePrefix)
		if !ok {
			return dynamicCallback{}, false
		}
		mode = flowUpdate
	}
	step, err := strconv.Atoi(rest)
	if err != nil || step < 1 {
		return dynamicCallback{}, false
	}
	return dynamicCallback{Mode: mode, Step: step}, true
}
