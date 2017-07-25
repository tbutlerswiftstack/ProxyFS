package swiftclient

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/swiftstack/ProxyFS/stats"
)

const swiftVersion = "v1"

func acquireChunkedConnection() (tcpConn *net.TCPConn, err error) {
	tcpConn = <-globals.chunkedConnectionPool

	if nil == tcpConn {
		tcpConn, err = net.DialTCP("tcp4", nil, globals.noAuthTCPAddr)
		if nil != err {
			globals.chunkedConnectionPool <- globals.nilTCPConn
			return
		}

		err = tcpConn.SetKeepAlive(true)
		if nil != err {
			globals.chunkedConnectionPool <- globals.nilTCPConn
			return
		}

		stats.IncrementOperations(&stats.SwiftChunkedConnsCreateOps)
	} else {
		err = nil

		stats.IncrementOperations(&stats.SwiftChunkedConnsReuseOps)
	}

	return
}

func releaseChunkedConnection(tcpConn *net.TCPConn, keepAlive bool) {
	if keepAlive {
		globals.chunkedConnectionPool <- tcpConn
	} else {
		_ = tcpConn.Close()
		globals.chunkedConnectionPool <- globals.nilTCPConn
	}
}

func acquireNonChunkedConnection() (tcpConn *net.TCPConn, err error) {
	tcpConn = <-globals.nonChunkedConnectionPool

	if nil == tcpConn {
		tcpConn, err = net.DialTCP("tcp4", nil, globals.noAuthTCPAddr)
		if nil != err {
			globals.nonChunkedConnectionPool <- globals.nilTCPConn
			return
		}

		err = tcpConn.SetKeepAlive(true)
		if nil != err {
			globals.nonChunkedConnectionPool <- globals.nilTCPConn
			return
		}

		stats.IncrementOperations(&stats.SwiftNonchunkedConnsCreateOps)
	} else {
		err = nil

		stats.IncrementOperations(&stats.SwiftNonchunkedConnsReuseOps)
	}

	return
}

func releaseNonChunkedConnection(tcpConn *net.TCPConn, keepAlive bool) {
	if keepAlive {
		globals.nonChunkedConnectionPool <- tcpConn
	} else {
		_ = tcpConn.Close()
		globals.nonChunkedConnectionPool <- globals.nilTCPConn
	}
}

func writeBytesToTCPConn(tcpConn *net.TCPConn, buf []byte) (err error) {
	var (
		bufPos  = int(0)
		written int
	)

	for bufPos < len(buf) {
		written, err = tcpConn.Write(buf[bufPos:])
		if nil != err {
			return
		}

		bufPos += written
	}

	err = nil
	return
}

func writeHTTPRequestLineAndHeaders(tcpConn *net.TCPConn, method string, path string, headers map[string][]string) (err error) {
	var (
		bytesBuffer      bytes.Buffer
		headerName       string
		headerValue      string
		headerValueIndex int
		headerValues     []string
	)

	_, _ = bytesBuffer.WriteString(method + " " + path + " HTTP/1.1\r\n")

	_, _ = bytesBuffer.WriteString("Host: " + globals.noAuthStringAddr + "\r\n")
	_, _ = bytesBuffer.WriteString("User-Agent: ProxyFS\r\n")

	for headerName, headerValues = range headers {
		_, _ = bytesBuffer.WriteString(headerName + ": ")
		for headerValueIndex, headerValue = range headerValues {
			if 0 == headerValueIndex {
				_, _ = bytesBuffer.WriteString(headerValue)
			} else {
				_, _ = bytesBuffer.WriteString(", " + headerValue)
			}
		}
		_, _ = bytesBuffer.WriteString("\r\n")
	}

	_, _ = bytesBuffer.WriteString("\r\n")

	err = writeBytesToTCPConn(tcpConn, bytesBuffer.Bytes())

	return
}

func writeHTTPPutChunk(tcpConn *net.TCPConn, buf []byte) (err error) {
	err = writeBytesToTCPConn(tcpConn, []byte(fmt.Sprintf("%X\r\n", len(buf))))
	if nil != err {
		return
	}

	if 0 < len(buf) {
		err = writeBytesToTCPConn(tcpConn, buf)
		if nil != err {
			return
		}
	}

	err = writeBytesToTCPConn(tcpConn, []byte(fmt.Sprintf("\r\n")))

	return
}

func readByteFromTCPConn(tcpConn *net.TCPConn) (b byte, err error) {
	var (
		numBytesRead int
		oneByteBuf   = []byte{byte(0)}
	)

	for {
		numBytesRead, err = tcpConn.Read(oneByteBuf)
		if nil != err {
			return
		}

		if 1 == numBytesRead {
			b = oneByteBuf[0]
			err = nil
			return
		}
	}
}

func readBytesFromTCPConn(tcpConn *net.TCPConn, bufLen int) (buf []byte, err error) {
	var (
		numBytesRead int
		bufPos       = int(0)
	)

	buf = make([]byte, bufLen)

	for bufPos < bufLen {
		numBytesRead, err = tcpConn.Read(buf[bufPos:])
		if nil != err {
			return
		}

		bufPos += numBytesRead
	}

	err = nil
	return
}

func readHTTPEmptyLineCRLF(tcpConn *net.TCPConn) (err error) {
	var (
		b byte
	)

	b, err = readByteFromTCPConn(tcpConn)
	if nil != err {
		return
	}
	if '\r' != b {
		err = fmt.Errorf("readHTTPEmptyLineCRLF() didn't find the expected '\\r'")
		return
	}

	b, err = readByteFromTCPConn(tcpConn)
	if nil != err {
		return
	}
	if '\n' != b {
		err = fmt.Errorf("readHTTPEmptyLineCRLF() didn't find the expected '\\n'")
		return
	}

	err = nil
	return
}

func readHTTPLineCRLF(tcpConn *net.TCPConn) (line string, err error) {
	var (
		b           byte
		bytesBuffer bytes.Buffer
	)

	for {
		b, err = readByteFromTCPConn(tcpConn)
		if nil != err {
			return
		}

		if '\r' == b {
			b, err = readByteFromTCPConn(tcpConn)
			if nil != err {
				return
			}

			if '\n' != b {
				err = fmt.Errorf("readHTTPLine() expected '\\n' after '\\r' to terminate line")
				return
			}

			line = bytesBuffer.String()
			err = nil
			return
		}

		err = bytesBuffer.WriteByte(b)
		if nil != err {
			return
		}
	}
}

func readHTTPLineLF(tcpConn *net.TCPConn) (line string, err error) {
	var (
		b           byte
		bytesBuffer bytes.Buffer
	)

	for {
		b, err = readByteFromTCPConn(tcpConn)
		if nil != err {
			return
		}

		if '\n' == b {
			line = bytesBuffer.String()
			err = nil
			return
		}

		err = bytesBuffer.WriteByte(b)
		if nil != err {
			return
		}
	}
}

func readHTTPStatusAndHeaders(tcpConn *net.TCPConn) (httpStatus int, headers map[string][]string, err error) {
	var (
		colonSplit      []string
		commaSplit      []string
		commaSplitIndex int
		commaSplitValue string
		line            string
	)

	line, err = readHTTPLineCRLF(tcpConn)
	if nil != err {
		return
	}

	if len(line) < len("HTTP/1.1 XXX") {
		err = fmt.Errorf("readHTTPStatusAndHeaders() expected StatusLine beginning with \"HTTP/1.1 XXX\"")
		return
	}

	if !strings.HasPrefix(line, "HTTP/1.1 ") {
		err = fmt.Errorf("readHTTPStatusAndHeaders() expected StatusLine beginning with \"HTTP/1.1 XXX\"")
		return
	}

	httpStatus, err = strconv.Atoi(line[len("HTTP/1.1 ") : len("HTTP/1.1 ")+len("XXX")])
	if nil != err {
		return
	}

	headers = make(map[string][]string)

	for {
		line, err = readHTTPLineCRLF(tcpConn)
		if nil != err {
			return
		}

		if 0 == len(line) {
			return
		}

		colonSplit = strings.SplitN(line, ":", 2)
		if 2 != len(colonSplit) {
			err = fmt.Errorf("readHTTPStatusAndHeaders() expected HeaderLine")
			return
		}

		commaSplit = strings.Split(colonSplit[1], ",")

		for commaSplitIndex, commaSplitValue = range commaSplit {
			commaSplit[commaSplitIndex] = strings.TrimSpace(commaSplitValue)
		}

		headers[colonSplit[0]] = commaSplit
	}
}

func parseContentRange(headers map[string][]string) (firstByte int64, lastByte int64, objectSize int64, err error) {
	// A Content-Range header is of the form a-b/n, where a, b, and n
	// are all positive integers
	bytesPrefix := "bytes "

	values, ok := headers["Content-Range"]
	if !ok {
		err = fmt.Errorf("Content-Range header not present")
		return
	} else if ok && 1 != len(values) {
		err = fmt.Errorf("expected only one value for Content-Range header")
		return
	}

	if !strings.HasPrefix(values[0], bytesPrefix) {
		err = fmt.Errorf("malformed Content-Range header (doesn't start with %v)", bytesPrefix)
	}

	parts := strings.SplitN(values[0][len(bytesPrefix):], "/", 2)
	if len(parts) != 2 {
		err = fmt.Errorf("malformed Content-Range header (no slash)")
		return
	}

	byteIndices := strings.SplitN(parts[0], "-", 2)
	if len(byteIndices) != 2 {
		err = fmt.Errorf("malformed Content-Range header (no dash)")
		return
	}

	firstByte, err = strconv.ParseInt(byteIndices[0], 10, 64)
	if err != nil {
		return
	}

	lastByte, err = strconv.ParseInt(byteIndices[1], 10, 64)
	if err != nil {
		return
	}

	objectSize, err = strconv.ParseInt(parts[1], 10, 64)
	return
}

func parseContentLength(headers map[string][]string) (contentLength int, err error) {
	var (
		contentLengthAsValues []string
		ok                    bool
	)

	contentLengthAsValues, ok = headers["Content-Length"]

	if ok {
		if 1 != len(contentLengthAsValues) {
			err = fmt.Errorf("parseContentLength() expected Content-Length HeaderLine with single value")
			return
		}

		contentLength, err = strconv.Atoi(contentLengthAsValues[0])
		if nil != err {
			err = fmt.Errorf("parseContentLength() could not parse Content-Length HeaderLine value: \"%s\"", contentLengthAsValues[0])
			return
		}

		if 0 > contentLength {
			err = fmt.Errorf("parseContentLength() could not parse Content-Length HeaderLine value: \"%s\"", contentLengthAsValues[0])
			return
		}
	} else {
		contentLength = 0
	}

	return
}

func parseTransferEncoding(headers map[string][]string) (chunkedTransfer bool) {
	var (
		transferEncodingAsValues []string
		ok                       bool
	)

	transferEncodingAsValues, ok = headers["Transfer-Encoding"]
	if !ok {
		chunkedTransfer = false
		return
	}

	if 1 != len(transferEncodingAsValues) {
		chunkedTransfer = false
		return
	}

	if "chunked" == transferEncodingAsValues[0] {
		chunkedTransfer = true
	} else {
		chunkedTransfer = false
	}

	return
}

func parseConnection(headers map[string][]string) (connectionStillOpen bool) {
	var (
		connectionAsValues []string
		ok                 bool
	)

	connectionAsValues, ok = headers["Connection"]
	if !ok {
		connectionStillOpen = true
		return
	}

	if 1 != len(connectionAsValues) {
		connectionStillOpen = true
		return
	}

	if "close" == connectionAsValues[0] {
		connectionStillOpen = false
	} else {
		connectionStillOpen = true
	}

	return
}

func readHTTPPayloadLines(tcpConn *net.TCPConn, headers map[string][]string) (lines []string, err error) {
	var (
		buf                  []byte
		bufCurrentPosition   int
		bufLineStartPosition int
		contentLength        int
	)

	contentLength, err = parseContentLength(headers)
	if nil != err {
		return
	}

	lines = make([]string, 0)

	if 0 < contentLength {
		buf, err = readBytesFromTCPConn(tcpConn, contentLength)
		if nil != err {
			return
		}

		bufLineStartPosition = 0
		bufCurrentPosition = 0

		for bufCurrentPosition < contentLength {
			if '\n' == buf[bufCurrentPosition] {
				if 2 > (bufCurrentPosition - bufLineStartPosition) {
					err = fmt.Errorf("readHTTPPayloadLines() unexpectedly found an empty line in Payload")
					return
				}

				lines = append(lines, string(buf[bufLineStartPosition:bufCurrentPosition]))

				bufLineStartPosition = bufCurrentPosition + 1
			}

			bufCurrentPosition++
		}

		if bufLineStartPosition != bufCurrentPosition {
			err = fmt.Errorf("readHTTPPayloadLines() unexpectedly found a non-terminated line in Payload")
			return
		}
	}

	err = nil
	return
}

func readHTTPChunk(tcpConn *net.TCPConn) (chunk []byte, err error) {
	var (
		chunkLenAsInt    int
		chunkLenAsUint64 uint64
		line             string
	)

	line, err = readHTTPLineCRLF(tcpConn)
	if nil != err {
		return
	}

	chunkLenAsUint64, err = strconv.ParseUint(line, 16, 32)
	if nil != err {
		return
	}

	chunkLenAsInt = int(chunkLenAsUint64)

	if 0 == chunkLenAsInt {
		chunk = make([]byte, 0)
	} else {
		chunk, err = readBytesFromTCPConn(tcpConn, chunkLenAsInt)
		if nil != err {
			return
		}
	}

	err = readHTTPEmptyLineCRLF(tcpConn)

	return
}