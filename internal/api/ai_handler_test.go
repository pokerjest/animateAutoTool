package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/ai"
	"github.com/stretchr/testify/assert"
)

func TestTruncateChatHistoryShortHistoryUnchanged(t *testing.T) {
	initial := []ai.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}

	assert.Equal(t, initial, truncateChatHistory(initial), "history under 25 messages should not be truncated")
}

func TestTruncateChatHistoryDropsOldestPreservesSystem(t *testing.T) {
	// 1 system + 40 alternating user/assistant turns = 41 messages, well over 25.
	msgs := []ai.ChatMessage{{Role: "system", Content: "sys"}}
	for i := 0; i < 20; i++ {
		msgs = append(msgs,
			ai.ChatMessage{Role: "user", Content: fmt.Sprintf("u%d", i)},
			ai.ChatMessage{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
		)
	}
	truncated := truncateChatHistory(msgs)

	assert.LessOrEqual(t, len(truncated), 26,
		"after truncation should keep ~25 messages plus the system prompt")
	assert.Equal(t, "system", truncated[0].Role,
		"system prompt must be preserved at index 0")
	assert.Equal(t, "user", truncated[1].Role,
		"truncation should snap to a user turn so the dialogue starts cleanly")
}

func TestTruncateChatHistoryWithoutSystemPrompt(t *testing.T) {
	msgs := []ai.ChatMessage{}
	for i := 0; i < 40; i++ {
		msgs = append(msgs,
			ai.ChatMessage{Role: "user", Content: fmt.Sprintf("u%d", i)},
			ai.ChatMessage{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
		)
	}
	truncated := truncateChatHistory(msgs)

	assert.LessOrEqual(t, len(truncated), 25,
		"without system message, history should be capped at maxMessages")
	assert.Equal(t, "user", truncated[0].Role,
		"truncated tail should still begin on a user turn")
}

func TestChatBubbleEscapesHTML(t *testing.T) {
	html := chatBubble("assistant", `<img src=x onerror=alert(1)>`+"\nnext")

	assert.NotContains(t, html, `<img src=x onerror=alert(1)>`)
	assert.Contains(t, html, `&lt;img src=x onerror=alert(1)&gt;`)
	assert.True(t, strings.Contains(html, "<br>next"), "newlines should remain visible as line breaks")
}
