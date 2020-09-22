package ext

import "testing"

func TestIndirectBlocksCalculation(t *testing.T) {

	// If there are 12 blocks or fewer no indirect blocks are necessary.
	if calculateNumberOfIndirectBlocks(0) != 0 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 0 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1) != 0 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(7) != 0 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 7 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(12) != 0 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 12 blocks incorrectly")
	}

	// If there are more than 12 blocks but no more than 12 + refsPerBlock there
	// should be exactly one indirect block.
	if calculateNumberOfIndirectBlocks(13) != 1 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 13 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(128) != 1 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 128 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1024) != 1 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1024 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1036) != 1 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1036 blocks incorrectly")
	}

	// If there are more than 12 + refsPerBlock we are looking at multiple
	// indirect blocks.
	if calculateNumberOfIndirectBlocks(1037) != 3 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1037 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1049612) != 1026 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1049612 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1049613) != 1029 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1049613 blocks incorrectly")
	}

}

func TestBlockTypeCalculation(t *testing.T) {

	// If there are 12 blocks or fewer no indirect blocks are necessary.
	if blockType(0) != 0 {
		t.Fatalf("blockType calculates block 0 incorrectly")
	}

	if blockType(1) != 0 {
		t.Fatalf("blockType calculates block 1 incorrectly")
	}

	if blockType(7) != 0 {
		t.Fatalf("blockType calculates block 7 incorrectly")
	}

	if blockType(11) != 0 {
		t.Fatalf("blockType calculates block 11 incorrectly")
	}

	// Moving into the first indirect region.
	if blockType(12) != 1 {
		t.Fatalf("blockType calculates block 12 incorrectly")
	}

	if blockType(13) != 0 {
		t.Fatalf("blockType calculates block 13 incorrectly")
	}

	if blockType(128) != 0 {
		t.Fatalf("blockType calculates block 128 incorrectly")
	}

	if blockType(1024) != 0 {
		t.Fatalf("blockType calculates block 1024 incorrectly")
	}

	if blockType(1036) != 0 {
		t.Fatalf("blockType calculates block 1036 incorrectly")
	}

	// Moving into second indirect region
	if blockType(1037) != 2 {
		t.Fatalf("blockType calculates block 1037 incorrectly")
	}
}
