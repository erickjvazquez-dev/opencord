package chat

import "testing"

func TestChannelNameRegex(t *testing.T) {
	valid := []string{"ab", "general", "off-topic", "dev_2", "a1_b-2", "voice-chat"}
	invalid := []string{
		"a",                                   // too short
		"",                                    // empty
		"Has-Caps",                            // uppercase not allowed
		"white space",                         // spaces not allowed
		"way-too-long-channel-name-exceeding", // > 32 chars
		"emoji-😀",                            // non-ascii
		"dots.not.allowed",                    // '.' not in set
	}
	for _, v := range valid {
		if !channelNameRe.MatchString(v) {
			t.Errorf("expected %q to be a valid channel name", v)
		}
	}
	for _, v := range invalid {
		if channelNameRe.MatchString(v) {
			t.Errorf("expected %q to be an invalid channel name", v)
		}
	}
}
