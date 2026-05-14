package ail

import "strings"

func isImageMime(mime string) bool {
	return strings.HasPrefix(strings.ToLower(mime), "image/")
}

func isAudioMime(mime string) bool {
	return strings.HasPrefix(strings.ToLower(mime), "audio/")
}

func isVideoMime(mime string) bool {
	return strings.HasPrefix(strings.ToLower(mime), "video/")
}
