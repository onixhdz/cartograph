package extractors

import (
	"testing"
)

const testSymServerContent = "Server"

func TestExtractFile_PopulatesContent(t *testing.T) {
	source := []byte("package main\n\nfunc Hello() {\n\tfmt.Println(\"hello\")\n}\n\nfunc World() {\n\tfmt.Println(\"world\")\n}\n")

	result, err := ExtractFile("/test/main.go", source, "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	if len(result.Symbols) == 0 {
		t.Fatal("expected at least one symbol")
	}

	for _, sym := range result.Symbols {
		if sym.Content == "" {
			t.Errorf("symbol %q has empty Content", sym.Name)
		}
		if sym.Name == "Hello" {
			if len(sym.Content) < 10 {
				t.Errorf("Hello content too short: %q", sym.Content)
			}
			if sym.Content[:10] != "func Hello" {
				t.Errorf("expected Hello content to start with 'func Hello', got %q", sym.Content[:10])
			}
		}
		if sym.Name == "World" {
			if len(sym.Content) < 10 {
				t.Errorf("World content too short: %q", sym.Content)
			}
		}
	}
}

func TestExtractFile_ContentForClass(t *testing.T) {
	source := []byte("class UserService:\n    def get_user(self, id):\n        return id\n\n    def delete_user(self, id):\n        pass\n")

	result, err := ExtractFile("/test/app.py", source, "python")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	var classContent string
	for _, sym := range result.Symbols {
		if sym.Name == "UserService" {
			classContent = sym.Content
			break
		}
	}
	if classContent == "" {
		t.Fatal("expected UserService class to have Content")
	}
	if len(classContent) < 20 {
		t.Errorf("UserService content too short: %q", classContent)
	}
}

func TestExtractFile_ContentForStruct(t *testing.T) {
	source := []byte("package main\n\ntype Server struct {\n\tHost string\n\tPort int\n}\n")

	result, err := ExtractFile("/test/server.go", source, "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	var structContent string
	for _, sym := range result.Symbols {
		if sym.Name == testSymServerContent {
			structContent = sym.Content
			break
		}
	}
	if structContent == "" {
		t.Fatal("expected Server struct to have Content")
	}
	if len(structContent) < 10 {
		t.Errorf("Server struct content too short: %q", structContent)
	}
}

func TestParseFiles_CustomReader(t *testing.T) {
	// Verify ParseOptions.ReadFile is used instead of os.ReadFile.
	readCalled := false
	files := []FileInput{
		{Path: "/virtual/main.go", Language: "go"},
	}

	result := ParseFiles(files, ParseOptions{
		Workers: 1,
		ReadFile: func(path string) ([]byte, error) {
			readCalled = true
			if path != "/virtual/main.go" {
				t.Errorf("unexpected path: %s", path)
			}
			return []byte("package main\n\nfunc Hello() {}\n"), nil
		},
	})

	if !readCalled {
		t.Error("expected custom ReadFile to be called")
	}
	if len(result.Symbols) == 0 {
		t.Error("expected symbols from custom reader")
	}
}

func TestParseFiles_DefaultReader(t *testing.T) {
	// With nil ReadFile, should fall back to os.ReadFile.
	// Pass a non-existent file — should get an error, not a panic.
	files := []FileInput{
		{Path: "/nonexistent/main.go", Language: "go"},
	}

	result := ParseFiles(files, ParseOptions{
		Workers:  1,
		ReadFile: nil, // default to os.ReadFile
	})

	// Should have an error for the non-existent file, not a panic.
	if len(result.Errors) == 0 {
		t.Error("expected error for non-existent file with default reader")
	}
}
