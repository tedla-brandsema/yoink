package yoink_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/tedla-brandsema/yoink"
)

func ExampleParseWithOptions() {
	result, err := yoink.ParseWithOptions(context.Background(), strings.NewReader("A document"), "document.md", yoink.Options{MaxDepth: -1})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(result)
	// Output: A document
}
