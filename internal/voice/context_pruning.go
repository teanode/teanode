package voice

import "github.com/teanode/teanode/internal/providers"

func pruneVoiceContext(messages []providers.ChatMessage, maxTokens int) []providers.ChatMessage {
	if len(messages) == 0 || maxTokens <= 0 {
		return messages
	}

	estimateTokens := func(m providers.ChatMessage) int {
		switch c := m.Content.(type) {
		case string:
			return len(c)/4 + 1
		default:
			return 1
		}
	}

	// Separate the system prompt from conversation messages.
	systemMessages := make([]providers.ChatMessage, 0, 2)
	convMessages := make([]providers.ChatMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			systemMessages = append(systemMessages, m)
		} else {
			convMessages = append(convMessages, m)
		}
	}

	// Compute tokens consumed by system messages (always kept).
	systemTokens := 0
	for _, m := range systemMessages {
		systemTokens += estimateTokens(m)
	}

	remaining := maxTokens - systemTokens
	if remaining <= 0 {
		// System prompt alone exceeds budget — return as-is.
		return messages
	}

	// Always keep the last 2 conversation messages.
	minKeep := 2
	if len(convMessages) <= minKeep {
		return messages
	}

	guaranteed := convMessages[len(convMessages)-minKeep:]
	candidates := convMessages[:len(convMessages)-minKeep]

	// Count tokens in guaranteed messages.
	for _, m := range guaranteed {
		remaining -= estimateTokens(m)
	}

	// Walk candidates newest-first and include as many as fit.
	kept := make([]providers.ChatMessage, 0, len(candidates))
	for i := len(candidates) - 1; i >= 0; i-- {
		t := estimateTokens(candidates[i])
		if remaining-t < 0 {
			break
		}
		remaining -= t
		kept = append(kept, candidates[i])
	}

	dropped := len(candidates) - len(kept)
	if dropped == 0 {
		return messages
	}

	// Reverse kept (we built it newest-first).
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	keptTotal := len(systemMessages) + len(kept) + len(guaranteed)
	estimatedKept := maxTokens - remaining
	pipelineLog.Debugf("voice context pruned: kept=%d dropped=%d estimated_tokens=%d", keptTotal, dropped, estimatedKept)

	result := make([]providers.ChatMessage, 0, keptTotal)
	result = append(result, systemMessages...)
	result = append(result, kept...)
	result = append(result, guaranteed...)
	return result
}
