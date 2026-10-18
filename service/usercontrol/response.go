package usercontrol

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// WriteStaticResponse runs after validation and access/quota checks but before
// pre-consume or upstream invocation. It never produces a billable usage record.
func WriteStaticResponse(c *gin.Context) (bool, error) {
	value, ok := c.Get(runtimeKey)
	if !ok {
		return false, nil
	}
	s := value.(*requestState)
	r := s.Response
	if r == nil {
		return false, nil
	}
	var request struct {
		Stream bool `json:"stream"`
	}
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return false, err
	}
	if request.Stream && r.Mode == "json" {
		return false, fmt.Errorf("synthetic JSON responses require stream=false")
	}
	path := c.Request.URL.Path
	chat := strings.HasSuffix(path, "/chat/completions")
	responses := strings.HasSuffix(path, "/responses")
	claude := strings.HasSuffix(path, "/messages")
	if r.Mode == "text" && !chat && !responses && !claude {
		return false, fmt.Errorf("unsupported synthetic text protocol")
	}
	s.Synthetic = true
	c.Header("X-New-API-Simulated", "true")
	c.Header("Cache-Control", "no-store")
	if r.Mode == "error" {
		status := r.Status
		if status == 0 {
			status = http.StatusTooManyRequests
		}
		code := r.ErrorCode
		if code == "" {
			code = "user_control_simulated"
		}
		errorBody := gin.H{"message": r.Body, "type": "user_control_simulated", "code": code}
		if claude {
			c.JSON(status, gin.H{"type": "error", "error": errorBody})
		} else {
			c.JSON(status, gin.H{"error": errorBody})
		}
		return true, nil
	}
	if r.Mode == "json" {
		c.Data(http.StatusOK, "application/json", []byte(r.Body))
		return true, nil
	}
	id := "simulated-" + common.GetUUID()
	modelName := c.GetString(OriginalModelKey)
	if !request.Stream {
		switch {
		case chat:
			c.JSON(200, gin.H{"id": id, "object": "chat.completion", "created": time.Now().Unix(), "model": modelName,
				"choices": []gin.H{{"index": 0, "message": gin.H{"role": "assistant", "content": r.Body}, "finish_reason": "stop"}},
				"usage":   gin.H{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}, "user_control_simulated": true})
		case responses:
			c.JSON(200, syntheticResponseObject(id, modelName, r.Body, "completed"))
		case claude:
			c.JSON(200, gin.H{"id": id, "type": "message", "role": "assistant", "model": modelName,
				"content": []gin.H{{"type": "text", "text": r.Body}}, "stop_reason": "end_turn", "stop_sequence": nil,
				"usage": gin.H{"input_tokens": 0, "output_tokens": 0}, "user_control_simulated": true})
		}
		return true, nil
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("X-Accel-Buffering", "no")
	writeEvent := func(name string, body gin.H) {
		encoded, _ := common.Marshal(body)
		if name != "" {
			_, _ = fmt.Fprintf(c.Writer, "event: %s\n", name)
		}
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", encoded)
	}
	switch {
	case chat:
		base := gin.H{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": modelName, "user_control_simulated": true}
		base["choices"] = []gin.H{{"index": 0, "delta": gin.H{"role": "assistant", "content": r.Body}, "finish_reason": nil}}
		writeEvent("", base)
		base["choices"] = []gin.H{{"index": 0, "delta": gin.H{}, "finish_reason": "stop"}}
		base["usage"] = gin.H{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
		writeEvent("", base)
		_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	case responses:
		itemID := id + "-message"
		part := gin.H{"type": "output_text", "text": r.Body, "annotations": []any{}}
		item := gin.H{"id": itemID, "type": "message", "role": "assistant", "status": "completed", "content": []gin.H{part}}
		events := []gin.H{
			{"type": "response.created", "response": syntheticResponseObject(id, modelName, "", "in_progress")},
			{"type": "response.output_item.added", "output_index": 0, "item": gin.H{"id": itemID, "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}},
			{"type": "response.content_part.added", "output_index": 0, "item_id": itemID, "content_index": 0, "part": gin.H{"type": "output_text", "text": "", "annotations": []any{}}},
			{"type": "response.output_text.delta", "output_index": 0, "item_id": itemID, "content_index": 0, "delta": r.Body},
			{"type": "response.output_text.done", "output_index": 0, "item_id": itemID, "content_index": 0, "text": r.Body},
			{"type": "response.content_part.done", "output_index": 0, "item_id": itemID, "content_index": 0, "part": part},
			{"type": "response.output_item.done", "output_index": 0, "item": item},
			{"type": "response.completed", "response": syntheticResponseObject(id, modelName, r.Body, "completed")},
		}
		for i, event := range events {
			event["sequence_number"] = i
			writeEvent(event["type"].(string), event)
		}
	case claude:
		writeEvent("message_start", gin.H{"type": "message_start", "message": gin.H{"id": id, "type": "message", "role": "assistant", "model": modelName, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": gin.H{"input_tokens": 0, "output_tokens": 0}}})
		writeEvent("content_block_start", gin.H{"type": "content_block_start", "index": 0, "content_block": gin.H{"type": "text", "text": ""}})
		writeEvent("content_block_delta", gin.H{"type": "content_block_delta", "index": 0, "delta": gin.H{"type": "text_delta", "text": r.Body}})
		writeEvent("content_block_stop", gin.H{"type": "content_block_stop", "index": 0})
		writeEvent("message_delta", gin.H{"type": "message_delta", "delta": gin.H{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": gin.H{"output_tokens": 0}})
		writeEvent("message_stop", gin.H{"type": "message_stop"})
	}
	c.Writer.Flush()
	return true, nil
}

func syntheticResponseObject(id, modelName, text, status string) gin.H {
	output := []gin.H{}
	if status == "completed" {
		output = append(output, gin.H{"id": id + "-message", "type": "message", "role": "assistant", "status": "completed", "content": []gin.H{{"type": "output_text", "text": text, "annotations": []any{}}}})
	}
	return gin.H{"id": id, "object": "response", "created_at": time.Now().Unix(), "model": modelName, "status": status, "output": output,
		"error": nil, "incomplete_details": nil, "metadata": gin.H{"user_control_simulated": "true"},
		"usage": gin.H{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}}
}
