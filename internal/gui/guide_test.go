package gui

import "testing"

func TestParseGuideSplitsOnTheSectionHeadings(t *testing.T) {
	got := parseGuide("# Lost quorum\n\nTell the trainee Vault returns 500.\n\n## Hints\n\nLook at the peers.\n\n## SOLUTION\n\nRecover with peers.json.\n\n## Docs\n\n- [Raft](https://example.com)\n")
	want := StepGuide{
		Briefing:   "# Lost quorum\n\nTell the trainee Vault returns 500.",
		Hints:      "Look at the peers.",
		Solution:   "Recover with peers.json.",
		References: "- [Raft](https://example.com)",
	}
	if got != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseGuideKeepsAnExplicitBriefingAndAnyOtherHeading(t *testing.T) {
	got := parseGuide("## Briefing\n\nStart here.\n\n## Background\n\nIt was the network.\n\n## Hints\n\nPing it.\n")
	if got.Briefing != "Start here.\n\n## Background\n\nIt was the network." || got.Hints != "Ping it." {
		t.Errorf("got %+v", got)
	}
}

func TestParseGuideIgnoresHeadingsInsideCodeFences(t *testing.T) {
	got := parseGuide("Run this:\n\n```sh\n## Hints\nvault status\n```\n\n## Hints\n\nReal hint.\n")
	if got.Hints != "Real hint." || got.Briefing == "" || got.Solution != "" {
		t.Errorf("got %+v", got)
	}
	if want := "Run this:\n\n```sh\n## Hints\nvault status\n```"; got.Briefing != want {
		t.Errorf("briefing = %q, want %q", got.Briefing, want)
	}
}

func TestParseGuideHandlesEmptyAndWindowsLineEndings(t *testing.T) {
	if got := parseGuide(""); got != (StepGuide{}) {
		t.Errorf("empty guide = %+v", got)
	}
	if got := parseGuide("Brief.\r\n\r\n## Solution\r\n\r\nDo it.\r\n"); got.Briefing != "Brief." || got.Solution != "Do it." {
		t.Errorf("got %+v", got)
	}
}
