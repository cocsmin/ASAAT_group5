package plm

import (
	"testing"

	"cakefactory/internal/model"
)

func TestOrderItemsRoundTrip(t *testing.T) {
	items := []model.Item{{Ingredient: "flour", Grams: 4000}, {Ingredient: "milk", Grams: 1000.5}}
	got := DecodeItems(encodeItems(items))
	if len(got) != 2 || got[0] != items[0] || got[1] != items[1] {
		t.Fatalf("round trip: %+v", got)
	}
}

func TestDecodeItemsIgnoresGarbage(t *testing.T) {
	got := DecodeItems("flour:4000;nonsense;;sugar:2000")
	if len(got) != 2 || got[0].Ingredient != "flour" || got[1].Ingredient != "sugar" {
		t.Fatalf("only well-formed parts are kept: %+v", got)
	}
	if len(DecodeItems("")) != 0 {
		t.Fatal("an empty list decodes to nothing")
	}
}

