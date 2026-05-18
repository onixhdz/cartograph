package treesitter

import "testing"

func TestSolidityRegisteredAsFirstClassGotreesitterLanguage(t *testing.T) {
	lang := DetectLanguageByName("solidity")
	if lang == nil {
		t.Fatal("expected registered solidity language")
	}
	if !lang.UsesFallback() {
		t.Fatal("expected solidity to use gotreesitter-backed parser runtime")
	}
	if got := DetectLanguage("Token.sol"); got == nil || LanguageName(got) != "solidity" {
		t.Fatalf("DetectLanguage(Token.sol) = %v, want solidity", got)
	}
	if fallback := DetectFallbackLanguage("Token.sol"); fallback == nil || LanguageName(fallback) != "solidity" {
		t.Fatalf("DetectFallbackLanguage(Token.sol) = %v, want solidity fallback grammar", fallback)
	}

	source := []byte(`pragma solidity ^0.8.20;
contract Token {
    event Transfer(address indexed from, address indexed to, uint256 amount);
    error Unauthorized(address caller);
    uint256 public totalSupply;
    constructor(uint256 initialSupply) {
        totalSupply = initialSupply;
    }
    function mint(address to, uint256 amount) external {
        emit Transfer(address(0), to, amount);
    }
}
`)

	parser := NewParser(lang)
	defer parser.Close()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse(solidity): %v", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("expected root node")
	}
	if root.HasError() {
		t.Fatalf("expected solidity parser to parse without errors, got %s", root.SExpr(lang))
	}
}
