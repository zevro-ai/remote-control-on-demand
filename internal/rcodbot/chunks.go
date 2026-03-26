package rcodbot

func splitChunks(text string, maxLen int) []string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(runes) > 0 {
		if len(runes) <= maxLen {
			chunks = append(chunks, string(runes))
			break
		}

		cut := maxLen
		for i := maxLen - 1; i >= maxLen/2; i-- {
			if runes[i] == '\n' || runes[i] == ' ' {
				cut = i + 1
				break
			}
		}

		chunks = append(chunks, string(runes[:cut]))
		runes = runes[cut:]
	}

	return chunks
}
