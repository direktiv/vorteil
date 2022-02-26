package ext4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestResizeData(t *testing.T) {

	x := int64(129)
	y := divide(x, DescriptorsPerBlock)

	s := &super{
		layout: layout{
			totalGroupDescriptors: x,
		},
	}

	data := s.resizeData()
	var addrs [BlockSize / 4]uint32
	err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &addrs)
	if err != nil {
		t.Error(err)
	}

	for i, addr := range addrs {
		if int64(i) < y {
			if addr != uint32(i+1) {
				goto fail
			}
		} else {
			if addr != 0 {
				goto fail
			}
		}
	}

	return

fail:

	t.Errorf("resizeData generated bad data")

}
