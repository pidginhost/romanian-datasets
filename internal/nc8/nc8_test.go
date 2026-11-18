package nc8

import (
	"strings"
	"testing"
)

const sample = `<?xml version="1.0" encoding="UTF-8" ?>
<InsNomenclature xmlns="http://www.intrastat.ro/xml/NomenclaturesSchema">
   <Id>CN</Id>
   <Version>2026</Version>
   <Item><Code>I</Code><NameEn>SECTION I - LIVE ANIMALS</NameEn><NameRo>SECTIUNEA I - ANIMALE VII</NameRo><Level>1</Level></Item>
   <Item><Code>01</Code><NameEn>CHAPTER 1 - LIVE ANIMALS</NameEn><NameRo>CAPITOLUL 1 - ANIMALE VII</NameRo><Level>2</Level><parentCode>I</parentCode></Item>
   <Item><Code>0101</Code><NameEn>Live horses</NameEn><NameRo>Cai, vii</NameRo><Level>4</Level><parentCode>01</parentCode></Item>
   <Item><Code>@1</Code><NameEn>Horses</NameEn><NameRo>Cai</NameRo><Level>5</Level><parentCode>0101</parentCode></Item>
   <Item><Code>01012100</Code><NameEn>Pure-bred breeding animals</NameEn><NameRo>Reproducatori   de rasa pura</NameRo><Level>6</Level><RefCode>p/st</RefCode><parentCode>@1</parentCode></Item>
   <Item><Code>010129</Code><NameEn>Other</NameEn><NameRo>Altele</NameRo><Level>6</Level><parentCode>@1</parentCode></Item>
</InsNomenclature>`

func TestRead(t *testing.T) {
	items, err := Read([]byte(sample), 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("%d items", len(items))
	}
	horse := items[4]
	if horse.Code != "01012100" || !horse.Declarable || *horse.Unit != "p/st" || *horse.Parent != "@1" || horse.NameRO != "Reproducatori de rasa pura" || horse.Level != 6 {
		t.Fatalf("%+v", horse)
	}
	if items[0].Parent != nil || items[5].Declarable || items[5].Unit != nil {
		t.Fatalf("%+v %+v", items[0], items[5])
	}
}

func TestReadStopsOnWhatItDoesNotKnow(t *testing.T) {
	for name, change := range map[string][2]string{
		"another year":     {"<Version>2026</Version>", "<Version>2025</Version>"},
		"an odd code":      {"<Code>010129</Code>", "<Code>0101-29</Code>"},
		"a code twice":     {"<Code>010129</Code>", "<Code>01012100</Code>"},
		"a missing parent": {"<parentCode>@1</parentCode></Item>\n</Ins", "<parentCode>@9</parentCode></Item>\n</Ins"},
		"no parent":        {"<Level>2</Level><parentCode>I</parentCode>", "<Level>2</Level>"},
		"no name":          {"<NameRo>Altele</NameRo>", "<NameRo></NameRo>"},
		"an odd level":     {"<Level>4</Level>", "<Level>patru</Level>"},
	} {
		page := strings.Replace(sample, change[0], change[1], 1)
		if page == sample {
			t.Fatalf("%s: the change did not apply", name)
		}
		if _, err := Read([]byte(page), 2026); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func source(page string) Source {
	return Source{
		Page: func() ([]byte, error) { return []byte(page), nil },
		File: func(y Year) ([]byte, error) { return []byte(sample), nil },
	}
}

const listing = `<a href="/doc/CN_2026.xml">- versiune xml</a>`

func TestDataset(t *testing.T) {
	d, err := Dataset(source(listing), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Files[0].Records) != 6 {
		t.Fatalf("%d records", len(d.Files[0].Records))
	}
}

func TestDatasetStops(t *testing.T) {
	if _, err := Dataset(source(listing+`<a href="/doc/CN_2027.xml">2027</a>`), nil); err == nil || !strings.Contains(err.Error(), "2027") {
		t.Errorf("a new year: %v", err)
	}
	if _, err := Dataset(source(`<a href="/doc/CN_2025.xml">2025</a>`), nil); err == nil {
		t.Error("a year no longer listed was read")
	}
	if _, err := Dataset(source(listing), map[string]bool{"2026|01012100": true, "2025|01012100": true}); err == nil || !strings.Contains(err.Error(), "2025|01012100") {
		t.Errorf("a gone item: %v", err)
	}
}
