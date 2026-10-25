package main

import "testing"

// The hooks here are from Wikipedia's Did you know archive, CC BY-SA 4.0.

func TestParseHook(t *testing.T) {
	tests := []struct {
		name, in, text, article string
	}{
		{
			"plain",
			`that at age 19, '''[[Felix Barker]]''' became the youngest [[drama]] critic working on [[Fleet Street]]?`,
			"that at age 19, Felix Barker became the youngest drama critic working on Fleet Street?",
			"Felix Barker",
		},
		{
			"bold italic with picture note",
			`that the species name of '''''[[Viburnum awabuki]]''''' ''(pictured)'' is derived from a Japanese word?`,
			"that the species name of Viburnum awabuki is derived from a Japanese word?",
			"Viburnum awabuki",
		},
		{
			"piped link",
			`that the [[Special Operations Executive|SOE]] operation '''[[Operation Bittern|Bittern]]''' was criticised?`,
			"that the SOE operation Bittern was criticised?",
			"Operation Bittern",
		},
		{
			"entities",
			`that the '''[[English cricket team in South Africa in 2009–10|England&ndash;South Africa series]]''' was close?`,
			"that the England–South Africa series was close?",
			"English cricket team in South Africa in 2009–10",
		},
		{
			"simple templates",
			`that '''[[EPO (singer)|EPO]]'''{{` + "`" + `s}} third album sold 5{{nbsp}}million copies in ''[[Japan]]''{{-?}}`,
			"that EPO's third album sold 5 million copies in Japan?",
			"EPO (singer)",
		},
		{
			"picture note with a template inside",
			`that five of the '''[[List of battlecruisers of Germany|seven German battlecruisers]]''' ''(''{{SMS|Von der Tann}} ''pictured)'' fought at [[Battle of Jutland|Jutland]]?`,
			"that five of the seven German battlecruisers fought at Jutland?",
			"List of battlecruisers of Germany",
		},
		{
			"picture note with nested parentheses",
			`that the '''[[Army of Flanders]]''' ''([[Siege of Breda (1624)]] pictured)'' was the longest standing army?`,
			"that the Army of Flanders was the longest standing army?",
			"Army of Flanders",
		},
		{
			"references and tags",
			`that '''[[Ngwenya Mine|the Ngwenya mine]]'''<ref name="a">Source</ref> is one of the <span class="x">oldest</span> mines?<ref name="b" />`,
			"that the Ngwenya mine is one of the oldest mines?",
			"Ngwenya Mine",
		},
		{
			"title as Wikipedia shows it",
			`that the '''[[barbeled_houndshark#Biology|houndshark]]''' has a spherical placenta?`,
			"that the houndshark has a spherical placenta?",
			"Barbeled houndshark",
		},
		{
			"bold inside the link",
			`that [[Utaawase|'''writing poetry''']] needed [[The Tale of Genji|Genji]]?`,
			"that writing poetry needed Genji?",
			"Utaawase",
		},
		{
			"editorial brackets",
			`that critics said '''''[[The Beautiful Letdown]]''''' was "nois[y]"?`,
			`that critics said The Beautiful Letdown was "nois[y]"?`,
			"The Beautiful Letdown",
		},
		{
			"that with a comma",
			`that, in 1922, '''[[Palacio de la Aduana (Málaga)|Palacio de la Aduana]]''' burned ''(pictured)''?`,
			"that, in 1922, Palacio de la Aduana burned?",
			"Palacio de la Aduana (Málaga)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, ok := parseHook(tt.in)
			if !ok {
				t.Fatalf("parseHook(%q) skipped it", tt.in)
			}
			if h.Text != tt.text {
				t.Errorf("text\n got: %q\nwant: %q", h.Text, tt.text)
			}
			if h.Article != tt.article {
				t.Errorf("article: got %q, want %q", h.Article, tt.article)
			}
		})
	}
}

func TestParseHookSkips(t *testing.T) {
	tests := []struct{ name, in string }{
		{"no bold link", `that [[Christ]] trampled a lion?`},
		{"unknown template", `that '''[[Ngwenya Mine]]''' is {{convert|10|km|mi}} long?`},
		{"unclosed template", `that '''[[Ngwenya Mine]]''' is {{nowrap|old?`},
		{"no that", `the '''[[Crimean Socialist Soviet Republic]]''' was headed by Lenin's brother?`},
		{"no question mark", `that '''[[Accordia]]''' became the first housing development to win the`},
		{"refers to its picture", `that the pictured '''[[rove beetle]]''' lives on beaches?`},
		{"maths", `that '''[[Penny graph]]''' has <math>n</math> pennies?`},
		{"unclosed link", `that '''[[Albert shako|abominable [shako]]]''' was hated?`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if h, ok := parseHook(tt.in); ok {
				t.Errorf("parseHook(%q) = %+v, want it skipped", tt.in, h)
			}
		})
	}
}

func TestParseArchive(t *testing.T) {
	page := `{{DYK archive header}}

==Did you know...==
<!--
* ... that '''[[Commented out]]''' is not a hook?
-->

===30 September 2026===
*'''''00:00, 30 September 2026 (UTC)'''''
{{main page image/DYK|image=Viburnum odoratissimum.jpg|caption=''Viburnum awabuki'' hedge}}
* ... that the species name of '''''[[Viburnum awabuki]]''''' ''(pictured)'' is derived from a Japanese word?
* ... that '''[[Sun Pictures]]''' released nearly 20 films between 2008 and 2010?
*... that the '''[[Ngwenya Mine]]''' is {{convert|10|km|mi}} long?

===29 September 2026===
*'''''00:00, 29 September 2026 (UTC)'''''
*…that '''[[Fritz Metzger]]''' designed a church shaped like a fish?
`
	hooks, skipped := parseArchive(page)
	want := []hook{
		{"that the species name of Viburnum awabuki is derived from a Japanese word?", "Viburnum awabuki"},
		{"that Sun Pictures released nearly 20 films between 2008 and 2010?", "Sun Pictures"},
		{"that Fritz Metzger designed a church shaped like a fish?", "Fritz Metzger"},
	}
	if len(hooks) != len(want) {
		t.Fatalf("got %d hooks %+v, want %d", len(hooks), hooks, len(want))
	}
	for i := range want {
		if hooks[i] != want[i] {
			t.Errorf("hook %d: got %+v, want %+v", i, hooks[i], want[i])
		}
	}
	if skipped != 1 {
		t.Errorf("skipped %d, want 1", skipped)
	}
}
