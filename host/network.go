package host

import (
	"bufio"
	"fmt"
	"github.com/evilsocket/islazy/str"
	"net"
	"os"
	"strings"
)

type NetworkINodes map[int]NetworkEntry

// https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/include/net/tcp_states.h
const (
	TCP_ESTABLISHED = iota + 1
	TCP_SYN_SENT
	TCP_SYN_RECV
	TCP_FIN_WAIT1
	TCP_FIN_WAIT2
	TCP_TIME_WAIT
	TCP_CLOSE
	TCP_CLOSE_WAIT
	TCP_LAST_ACK
	TCP_LISTEN
	TCP_CLOSING /* Now a valid state */
	TCP_NEW_SYN_RECV
)

var sockStates = map[uint]string{
	TCP_ESTABLISHED:  "ESTABLISHED",
	TCP_SYN_SENT:     "SYN_SENT",
	TCP_SYN_RECV:     "SYN_RECV",
	TCP_FIN_WAIT1:    "FIN_WAIT1",
	TCP_FIN_WAIT2:    "FIN_WAIT2",
	TCP_TIME_WAIT:    "TIME_WAIT",
	TCP_CLOSE:        "CLOSE",
	TCP_CLOSE_WAIT:   "CLOSE_WAIT",
	TCP_LAST_ACK:     "LAST_ACK",
	TCP_LISTEN:       "LISTENING",
	TCP_CLOSING:      "CLOSING",
	TCP_NEW_SYN_RECV: "NEW_SYN_RECV",
}

var sockTypes = map[uint]string{
	1: "SOCK_STREAM",
	2: "SOCK_DGRAM",
	5: "SOCK_SEQPACKET",
}

type NetworkEntry struct {
	Proto       string
	Type        uint
	TypeString  string
	Groups      string
	Path        string
	State       uint
	StateString string
	SrcIP       net.IP
	SrcPort     uint
	DstIP       net.IP
	DstPort     uint
	UserId      int
	INode       int
}

func (e NetworkEntry) String() string {
	if e.Proto == "unix" {
		// see about empty paths: https://stackoverflow.com/questions/820782/how-do-i-find-out-what-programs-on-the-other-end-of-a-local-socket
		if e.Path == "" {
			return fmt.Sprintf("(%s) %s inode=%d", e.Proto, e.TypeString, e.INode)
		}
		return fmt.Sprintf("(%s) %s path='%s'", e.Proto, e.TypeString, e.Path)
	} else if e.Proto == "netlink" {
		return fmt.Sprintf("(%s) groups=%s", e.Proto, e.Groups)
	} else if e.Proto == "udp" {
		if e.DstIP.String() == "0.0.0.0" {
			return fmt.Sprintf("(%s) %s:%d", e.Proto, e.SrcIP, e.SrcPort)
		}
	} else if e.State == TCP_LISTEN {
		return fmt.Sprintf("(%s) %s:%d", e.Proto, e.SrcIP, e.SrcPort)
	}

	return fmt.Sprintf("(%s) %s:%d <-> %s:%d", e.Proto, e.SrcIP, e.SrcPort, e.DstIP, e.DstPort)
}

func (e NetworkEntry) InfoString() string {
	if e.Proto == "unix" {
		if e.Path != "" {
			if info, err := os.Stat(e.Path); err == nil {
				return info.Mode().String()
			}
		}
	} else {
		return e.StateString
	}
	return ""
}

var (
	protocols = []string{"tcp", "tcp6", "udp", "udp6", "unix", "netlink"}
)

// /proc/net/tcp:
// sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
// 0:  0100007F:13AD 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 18083222
func parseIP(filename, line, protocol string) (entry NetworkEntry, err error) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return entry, fmt.Errorf("could not parse netstat line from %s (got %d fields): %s", filename, len(fields), line)
	}

	local := strings.Split(fields[1], ":")
	remote := strings.Split(fields[2], ":")
	sockState := hexToInt(fields[3])

	entry = NetworkEntry{
		Proto:       protocol,
		SrcIP:       hexToIP(local[0]),
		SrcPort:     hexToInt(local[1]),
		DstIP:       hexToIP(remote[0]),
		DstPort:     hexToInt(remote[1]),
		State:       sockState,
		StateString: sockStates[sockState],
		UserId:      decToInt(fields[7]),
		INode:       decToInt(fields[9]),
	}

	return entry, nil
}

// /proc/net/unix
// Num       RefCount Protocol Flags    Type St Inode Path
// 0000000000000000: 00000002 00000000 00010000 0001 01 28271 /run/user/1000/gnupg/S.dirmngr
func parseUnix(filename, line, protocol string) (entry NetworkEntry, err error) {
	fields := strings.Fields(line)
	num := len(fields)
	if num < 7 {
		return entry, fmt.Errorf("could not parse netstat line from %s (got %d fields): %s", filename, 7, line)
	}

	sockType := hexToInt(fields[4])
	sockState := hexToInt(fields[5])
	path := ""

	if num > 7 {
		path = fields[7]
	}

	entry = NetworkEntry{
		Proto:       protocol,
		Type:        sockType,
		TypeString:  sockTypes[sockType],
		State:       sockState,
		StateString: sockStates[sockState],
		INode:       decToInt(fields[6]),
		Path:        path,
	}

	return entry, nil
}

// /proc/net/netlink
// sk               Eth Pid        Groups   Rmem     Wmem     Dump     Locks     Drops     Inode
// 0000000000000000 0   2192944774 00000011 0        0        0        2         0         4842849
// 0000000000000000 0   2014       00000110 0        0        0        2         0         27854
func parseNetlink(filename, line, protocol string) (entry NetworkEntry, err error) {
	fields := strings.Fields(line)
	num := len(fields)
	if num < 10 {
		return entry, fmt.Errorf("could not parse netstat line from %s (got %d fields): %s", filename, 7, line)
	}

	entry = NetworkEntry{
		Proto:  protocol,
		Groups: fields[3],
		INode:  decToInt(fields[9]),
	}

	return entry, nil
}

// Parse scans and retrieves the opened connections, from /proc/net/ files
func parseNetworkForProtocol(proto string) ([]NetworkEntry, error) {
	filename := fmt.Sprintf("%s/net/%s", ProcFS, proto)
	fd, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	var entry NetworkEntry
	entries := make([]NetworkEntry, 0)
	scanner := bufio.NewScanner(fd)

	for lineno := 0; scanner.Scan(); lineno++ {
		// skip column names
		if lineno == 0 {
			continue
		}

		line := str.Trim(scanner.Text())
		if proto == "unix" {
			if entry, err = parseUnix(filename, line, proto); err != nil {
				panic(err)
			}
		} else if proto == "netlink" {
			if entry, err = parseNetlink(filename, line, proto); err != nil {
				panic(err)
			}
		} else {
			if entry, err = parseIP(filename, line, proto); err != nil {
				panic(err)
			}
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func parseNetworkInodes() (NetworkINodes, error) {
	byInode := make(NetworkINodes)
	for i := range protocols {
		if entries, err := parseNetworkForProtocol(protocols[i]); err != nil {
			return nil, err
		} else {
			for _, entry := range entries {
				byInode[entry.INode] = entry
			}
		}
	}
	return byInode, nil
}
