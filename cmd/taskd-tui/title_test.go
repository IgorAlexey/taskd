package main

import "testing"

func TestTableTitleLeadingNewlines(t *testing.T) {
	for _, tc := range []struct {
		name string
		task task
		want string
	}{
		{"leading newline", task{Body: "\n## Summary\nMore details"}, "## Summary"},
		{"leading whitespace", task{Body: "\n  \n\t\nTask with leading whitespace\nDescription"}, "Task with leading whitespace"},
		{"crlf", task{Body: "\r\n\r\nTask with CRLF\r\nDetails"}, "Task with CRLF"},
		{"blank body with asset", task{Body: "\n\n   \n", AssetPath: "assets/model.gltf"}, "assets/model.gltf"},
		{"standard title", task{Body: "Standard task title\nNext line"}, "Standard task title"},
	} {
		if got := taskTitle(tc.task); got != tc.want {
			t.Errorf("%s: taskTitle() = %q, want %q", tc.name, got, tc.want)
		}
	}
}
