package source

func utf16Width(text string) uint32 {
	var width uint32
	for _, r := range text {
		width++
		if r > 0xffff {
			width++
		}
	}

	return width
}
