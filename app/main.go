package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"strings"
)

type DNSMessage struct {
  Header
  Questions []Question
  Answer []RR
}

type Header struct {
  ID uint16
  QR uint8
  OPCODE uint8
  AA uint8
  TC uint8
  RD uint8
  RA uint8
  Z uint8
  RCODE uint8
  QDCOUNT uint16
  ANCOUNT uint16
  NSCOUNT uint16
  ARCOUNT uint16
}

type Question struct {
  Name string
  Type uint16
  Class uint16
}

type RR struct {
  Name string
  Type uint16
  Class uint16
  TTL uint32
  Length uint16
  Data []byte
}

func newRR(name string) RR {
  return RR {
    Name: name,
    Type: 1,
    Class: 1,
    TTL: 60,
    Length: 4,
    Data: []byte("\x08\x08\x08\x08"),
  }
}

func newQuestion(name string) Question {
  return Question {
    Name: name,
    Type: 1,
    Class: 1,
  }
}

func generateResponse(dnsRequest DNSMessage) DNSMessage {
  var responseRCODE uint8
  switch dnsRequest.OPCODE {
  case 0:
    responseRCODE = 0
  default:
    responseRCODE = 4
  }

  a := []RR {}
  q := []Question{}
  for _, reqQ := range(dnsRequest.Questions) {
    a = append(a, newRR(reqQ.Name))
    q = append(q, newQuestion(reqQ.Name))
  }

  h := Header {
    ID:  dnsRequest.ID,
    QR: 1,
    OPCODE: dnsRequest.OPCODE,
    AA: 0,
    TC: 0,
    RD: dnsRequest.RD,
    RA: dnsRequest.RA,
    Z: 0,
    RCODE: responseRCODE,
    QDCOUNT: uint16(len(q)),
    ANCOUNT: uint16(len(a)),
    NSCOUNT: 0,
    ARCOUNT: 0,
  }
  return DNSMessage{Questions: q, Header: h, Answer: a}
}

func generateRequest(originalRequest DNSMessage, singleQuestion Question) DNSMessage {
  a := []RR {}
  q := []Question{}
  q = append(q, singleQuestion)

  h := Header {
    ID:  originalRequest.ID,
    QR: 0,
    OPCODE: originalRequest.OPCODE,
    AA: 0,
    TC: 0,
    RD: originalRequest.RD,
    RA: originalRequest.RA,
    Z: 0,
    RCODE: 0,
    QDCOUNT: uint16(len(q)),
    ANCOUNT: uint16(len(a)),
    NSCOUNT: 0,
    ARCOUNT: 0,
  }
  return DNSMessage{Questions: q, Header: h, Answer: a}
}

func (q Question) serialize() []byte {
    buffer := new(bytes.Buffer)
    buffer.Write(serializeDomain(q.Name))
    binary.Write(buffer, binary.BigEndian, q.Type)
    binary.Write(buffer, binary.BigEndian, q.Class)
    return buffer.Bytes()
}

func (r RR) serialize() []byte {
    buffer := new(bytes.Buffer)
    buffer.Write(serializeDomain(r.Name))
    binary.Write(buffer, binary.BigEndian, r.Type)
    binary.Write(buffer, binary.BigEndian, r.Class)
    binary.Write(buffer, binary.BigEndian, r.TTL)
    binary.Write(buffer, binary.BigEndian, uint16(len(r.Data)))
    buffer.Write(r.Data)
    return buffer.Bytes()
}

func (h Header) serialize() []byte {
	buffer := make([]byte, 12)
	binary.BigEndian.PutUint16(buffer[0:2], h.ID)
	buffer[2] = (h.QR << 7) | (h.OPCODE << 3) | (h.AA << 2) | (h.TC << 1) | h.RD
  buffer[3] = (h.RA << 7) | (h.Z << 4) | h.RCODE	
	binary.BigEndian.PutUint16(buffer[4:6], h.QDCOUNT)
	binary.BigEndian.PutUint16(buffer[6:8], h.ANCOUNT)
	binary.BigEndian.PutUint16(buffer[8:10], h.NSCOUNT)
	binary.BigEndian.PutUint16(buffer[10:12], h.ARCOUNT)
	return buffer
}

func serializeDomain(domain string) []byte {
  buffer := []byte{}
  labels := strings.Split(domain, ".")
  for _, label := range(labels) {
    buffer = append(buffer, byte(len(label)))
    buffer = append(buffer, []byte(label)...)
  }
  buffer = append(buffer, '\x00')
  return buffer
}

func (m DNSMessage) serialize() []byte {
  buffer := new(bytes.Buffer)
  buffer.Write(m.Header.serialize())
  for _, q := range(m.Questions) {
    buffer.Write(q.serialize())
  }
  for _, r := range(m.Answer) {
    buffer.Write(r.serialize())
  }
  return buffer.Bytes()
}

func parseDNSMessage(data []byte) (DNSMessage, error) {
    var msg DNSMessage
    var err error

    reader := bytes.NewReader(data)

    hdr, err := parseHeader(data)
    if err != nil {
        return msg, err
    }
    msg.Header = *hdr

    _, _ = reader.Seek(12, io.SeekStart)
    for i := 0; i < int(msg.Header.QDCOUNT); i++ {
        var q Question
        q.Name, err = readName(reader)
        if err != nil {
            return msg, fmt.Errorf("failed to parse question name: %v", err)
        }
        err = binary.Read(reader, binary.BigEndian, &q.Type)
        if err != nil {
            return msg, fmt.Errorf("failed to parse question type: %v", err)
        }
        err = binary.Read(reader, binary.BigEndian, &q.Class)
        if err != nil {
            return msg, fmt.Errorf("failed to parse question class: %v", err)
        }
        msg.Questions = append(msg.Questions, q)
    }

    for i := 0; i < int(msg.Header.ANCOUNT); i++ {
        var rr RR
        rr.Name, err = readName(reader)
        if err != nil {
            return msg, fmt.Errorf("failed to parse RR name: %v", err)
        }
        err = binary.Read(reader, binary.BigEndian, &rr.Type)
        if err != nil {
            return msg, fmt.Errorf("failed to parse RR type: %v", err)
        }
        err = binary.Read(reader, binary.BigEndian, &rr.Class)
        if err != nil {
            return msg, fmt.Errorf("failed to parse RR class: %v", err)
        }
        err = binary.Read(reader, binary.BigEndian, &rr.TTL)
        if err != nil {
            return msg, fmt.Errorf("failed to parse RR TTL: %v", err)
        }
        err = binary.Read(reader, binary.BigEndian, &rr.Length)
        if err != nil {
            return msg, fmt.Errorf("failed to parse RR data length: %v", err)
        }
        rr.Data = make([]byte, rr.Length)
        _, err = reader.Read(rr.Data)
        if err != nil {
            return msg, fmt.Errorf("failed to parse RR data: %v", err)
        }
        msg.Answer = append(msg.Answer, rr)
    }

    return msg, nil
}

func parseHeader(data []byte) (*Header, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("header too short")
	}

	header := &Header{
		ID: binary.BigEndian.Uint16(data[:2]),
	}

	flags := binary.BigEndian.Uint16(data[2:4])
	header.QR = uint8(flags >> 15 & 0x01)
	header.OPCODE = uint8(flags >> 11 & 0x0F)
	header.AA = uint8(flags >> 10 & 0x01)
	header.TC = uint8(flags >> 9 & 0x01)
	header.RD = uint8(flags >> 8 & 0x01)
	header.RA = uint8(flags >> 7 & 0x01)
	header.Z = uint8(flags >> 4 & 0x07)
	header.RCODE = uint8(flags & 0x0F)

	header.QDCOUNT = binary.BigEndian.Uint16(data[4:6])
	header.ANCOUNT = binary.BigEndian.Uint16(data[6:8])
	header.NSCOUNT = binary.BigEndian.Uint16(data[8:10])
	header.ARCOUNT = binary.BigEndian.Uint16(data[10:12])

	return header, nil
}
func parseName(reader *bytes.Reader, offset int64) (string, error) {
    var nameParts []string
    reader.Seek(offset, io.SeekStart) // Move to the correct offset to start reading the name

    for {
        var length byte
        err := binary.Read(reader, binary.BigEndian, &length)
        if err != nil {
            return "", fmt.Errorf("failed to read name length: %v", err)
        }

        // Check if this is a pointer
        if length >= 192 {
            var offsetPart byte
            err := binary.Read(reader, binary.BigEndian, &offsetPart)
            if err != nil {
                return "", fmt.Errorf("failed to read the second part of the pointer: %v", err)
            }
            newOffset := int64(length&0x3F)<<8 + int64(offsetPart)
            // Recursively parse the name starting at the new offset
            partName, err := parseName(reader, newOffset)
            if err != nil {
                return "", err
            }
            nameParts = append(nameParts, partName)
            break 
        } else if length == 0 {
            break
        } else {
            labels := make([]byte, length)
            _, err = reader.Read(labels)
            if err != nil {
                return "", fmt.Errorf("failed to read name labels: %v", err)
            }
            nameParts = append(nameParts, string(labels))
        }
    }
    return strings.Join(nameParts, "."), nil
}

func readName(reader *bytes.Reader) (string, error) {
    currentOffset, _ := reader.Seek(0, io.SeekCurrent)
    return parseName(reader, currentOffset)
}

var resolverFlag = flag.String("resolver", ".", "Address of DNS server to forward to")

func main() {
  flag.Parse()

	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:2053")
	if err != nil {
		fmt.Println("Failed to resolve UDP address:", err)
		return
	}

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		fmt.Println("Failed to bind to address:", err)
		return
	}
	defer udpConn.Close()

	buf := make([]byte, 512)

	for {
		size, source, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Error receiving data:", err)
			break
		}

		dnsRequest, _ := parseDNSMessage(buf[:size])

    var response DNSMessage
    if *resolverFlag == "" {
      response = generateResponse(dnsRequest)
    } else {
      addr, _ := net.ResolveUDPAddr("udp", *resolverFlag)
      conn, _ := net.DialUDP("udp", nil, addr)

      // just overwrite the answers
      response = generateResponse(dnsRequest)
      response.Answer = []RR{}
      for _, reqQ := range dnsRequest.Questions {
        req := generateRequest(dnsRequest, reqQ)
        conn.Write(req.serialize())

        buffer := make([]byte, 512)
        conn.ReadFromUDP(buffer)

        dnsResponse, _ := parseDNSMessage(buffer)
        if len(dnsResponse.Answer) > 0 {
          response.Answer = append(response.Answer, dnsResponse.Answer[0])
        }
      }
      response.Header.ANCOUNT = uint16(len(response.Answer))
    }
    _, err = udpConn.WriteToUDP(response.serialize(), source)
    if err != nil {
      fmt.Println("Failed to send response:", err)
    }
	}
}
