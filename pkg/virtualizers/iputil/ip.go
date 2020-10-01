package iputil

import (
	"fmt"
	"net"
)

const (
	BaseAddr = "10.26.10.0"
	BaseMask = "24"
)

type ipStackItems []string

type IPStack struct {
	stack    ipStackItems
	stackNet *net.IPNet
}

func (s *IPStack) count() int {
	return len(s.stack)
}

func (s *IPStack) isEmpty() bool {
	return len(s.stack) == 0
}

func NewIPStack() *IPStack {

	s := &IPStack{}

	cidr := fmt.Sprintf("%s/%s", BaseAddr, BaseMask)

	ip, ipnet, _ := net.ParseCIDR(cidr)

	s.stackNet = ipnet

	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); inc(ip) {
		s.push(ip.String())
	}

	s.stack = reverse(s.stack[1 : len(s.stack)-1])

	return s
}

func (s *IPStack) push(str string) {

	// very expensive....we should really add push to the object in the stack
	for _, i := range s.stack {
		if i == str {
			return
		}
	}

	if s.stackNet.Contains(net.ParseIP(str)) {
		s.stack = append(s.stack, str)
	}

}

func (s *IPStack) Pop() string {
	if s.isEmpty() {
		return ""
	}

	index := len(s.stack) - 1
	element := (s.stack)[index]
	s.stack = (s.stack)[:index]
	return element
}

func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func reverse(numbers []string) []string {
	for i := 0; i < len(numbers)/2; i++ {
		j := len(numbers) - i - 1
		numbers[i], numbers[j] = numbers[j], numbers[i]
	}
	return numbers
}
