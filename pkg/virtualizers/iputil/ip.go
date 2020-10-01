package iputil

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/beeker1121/goque"
)

const (
	BaseAddr = "10.26.10.0"
	BridgeIP = "10.26.10.1"
	BaseMask = "24"
)

func NewIPStack() (*goque.Queue, error) {
	q, err := goque.OpenQueue(filepath.Join(os.TempDir(), "iputil"))
	if err != nil {
		return nil, err
	}
	_, err = q.Peek()
	if err != nil {
		// Queue is empty
		if strings.Contains(err.Error(), "Stack or queue is empty") {
			cidr := fmt.Sprintf("%s/%s", BaseAddr, BaseMask)
			ip, ipnet, _ := net.ParseCIDR(cidr)
			for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); inc(ip) {
				// Ignore the first 2 as one is used for the bridge device
				if ip.String() != "10.26.10.0" && ip.String() != "10.26.10.1" {
					q.EnqueueString(ip.String())
				}
			}
		} else {
			return nil, err
		}
	}

	return q, nil
}

func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
