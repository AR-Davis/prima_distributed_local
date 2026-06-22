package rpc

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// RPC command codes (from ggml-rpc.cpp enum rpc_cmd)
const (
	CmdAllocBuffer    = 0
	CmdGetAlignment   = 1
	CmdGetMaxSize     = 2
	CmdBufferGetBase  = 3
	CmdFreeBuffer     = 4
	CmdBufferClear    = 5
	CmdSetTensor      = 6
	CmdGetTensor      = 7
	CmdCopyTensor     = 8
	CmdGraphCompute   = 9
	CmdGetDeviceMemory = 10
)

// RPCTensor mirrors the C++ rpc_tensor struct (binary layout)
// Must match the memory layout exactly for wire compatibility.
// Total size: 64+4+8+4*4+4*4+4+32+4+8*6+8+8+64+4 = 268 bytes (with padding)
type RPCTensor struct {
	ID       uint64
	Type     uint32
	Buffer   uint64
	NE       [4]uint32 // GGML_MAX_DIMS = 4
	NB       [4]uint32 // GGML_MAX_DIMS = 4
	Op       uint32
	OpParams [16]int32 // GGML_MAX_OP_PARAMS / sizeof(int32) = 64/4 = 16
	Flags    int32
	Src      [6]uint64 // GGML_MAX_SRC = 6
	ViewSrc  uint64
	ViewOffs uint64
	Data     uint64
	Name     [64]byte // GGML_MAX_NAME = 64
	Padding  [4]byte
}

// DeviceMemory holds free/total memory info from an RPC node.
type DeviceMemory struct {
	Free  uint64
	Total uint64
}

// BufferInfo holds the result of allocating a buffer on a remote node.
type BufferInfo struct {
	RemotePtr uint64
	Size      uint64
}

// Client is a Go client for the ggml-rpc binary protocol.
// It connects to a prima.cpp rpc-server and can query device info,
// allocate buffers, and eventually send compute graphs.
type Client struct {
	addr    string
	mu      sync.Mutex
	conn    net.Conn
	timeout time.Duration
}

// NewClient creates a new RPC client for the given address (host:port).
func NewClient(addr string) *Client {
	return &Client{
		addr:    addr,
		timeout: 30 * time.Second,
	}
}

// Dial connects to the RPC server. Call this before any RPC commands.
func (c *Client) Dial() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.conn != nil {
		c.conn.Close()
	}
	
	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		return fmt.Errorf("rpc: dial %s: %w", c.addr, err)
	}
	c.conn = conn
	return nil
}

// Close closes the connection to the RPC server.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// IsAlive checks if the RPC server is reachable by attempting a TCP connection.
func (c *Client) IsAlive() (bool, time.Duration) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", c.addr, 5*time.Second)
	if err != nil {
		return false, 0
	}
	conn.Close()
	return true, time.Since(start)
}

// sendCommand sends an RPC command and reads the response.
// Protocol: | cmd (1 byte) | request_size (8 bytes LE) | request_data |
// Response: | response_size (8 bytes LE) | response_data |
func (c *Client) sendCommand(cmd byte, input []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.conn == nil {
		return nil, fmt.Errorf("rpc: not connected")
	}
	
	// Set write deadline
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, fmt.Errorf("rpc: set write deadline: %w", err)
	}
	
	// Send command byte
	if _, err := c.conn.Write([]byte{cmd}); err != nil {
		return nil, fmt.Errorf("rpc: write cmd: %w", err)
	}
	
	// Send request size (8 bytes, little-endian)
	sizeBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(sizeBuf, uint64(len(input)))
	if _, err := c.conn.Write(sizeBuf); err != nil {
		return nil, fmt.Errorf("rpc: write size: %w", err)
	}
	
	// Send request data
	if len(input) > 0 {
		if _, err := c.conn.Write(input); err != nil {
			return nil, fmt.Errorf("rpc: write data: %w", err)
		}
	}
	
	// Read response size (8 bytes, little-endian)
	if err := c.conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, fmt.Errorf("rpc: set read deadline: %w", err)
	}
	
	if _, err := io.ReadFull(c.conn, sizeBuf); err != nil {
		return nil, fmt.Errorf("rpc: read response size: %w", err)
	}
	
	respSize := binary.LittleEndian.Uint64(sizeBuf)
	
	// Sanity check: response shouldn't be enormous
	if respSize > 1<<30 { // 1 GiB max
		return nil, fmt.Errorf("rpc: response too large (%d bytes)", respSize)
	}
	
	// Read response data
	respData := make([]byte, respSize)
	if respSize > 0 {
		if _, err := io.ReadFull(c.conn, respData); err != nil {
			return nil, fmt.Errorf("rpc: read response data: %w", err)
		}
	}
	
	return respData, nil
}

// GetAlignment queries the RPC server for its memory alignment requirement.
// Returns the alignment in bytes.
func (c *Client) GetAlignment() (uint64, error) {
	resp, err := c.sendCommand(CmdGetAlignment, nil)
	if err != nil {
		return 0, err
	}
	if len(resp) < 8 {
		return 0, fmt.Errorf("rpc: alignment response too short (%d bytes)", len(resp))
	}
	return binary.LittleEndian.Uint64(resp), nil
}

// GetMaxSize queries the RPC server for the maximum buffer allocation size.
func (c *Client) GetMaxSize() (uint64, error) {
	resp, err := c.sendCommand(CmdGetMaxSize, nil)
	if err != nil {
		return 0, err
	}
	if len(resp) < 8 {
		return 0, fmt.Errorf("rpc: max_size response too short (%d bytes)", len(resp))
	}
	return binary.LittleEndian.Uint64(resp), nil
}

// GetDeviceMemory queries the RPC server for free and total device memory.
// Response format: | free (8 bytes) | total (8 bytes) |
func (c *Client) GetDeviceMemory() (*DeviceMemory, error) {
	resp, err := c.sendCommand(CmdGetDeviceMemory, nil)
	if err != nil {
		return nil, err
	}
	if len(resp) < 16 {
		return nil, fmt.Errorf("rpc: device_memory response too short (%d bytes)", len(resp))
	}
	
	free := binary.LittleEndian.Uint64(resp[0:8])
	total := binary.LittleEndian.Uint64(resp[8:16])
	
	return &DeviceMemory{
		Free:  free,
		Total: total,
	}, nil
}

// AllocBuffer allocates a buffer on the remote RPC server.
// Input format: | size (8 bytes) |
// Response format: | remote_ptr (8 bytes) | remote_size (8 bytes) |
func (c *Client) AllocBuffer(size uint64) (*BufferInfo, error) {
	input := make([]byte, 8)
	binary.LittleEndian.PutUint64(input, size)
	
	resp, err := c.sendCommand(CmdAllocBuffer, input)
	if err != nil {
		return nil, err
	}
	if len(resp) < 16 {
		return nil, fmt.Errorf("rpc: alloc_buffer response too short (%d bytes)", len(resp))
	}
	
	ptr := binary.LittleEndian.Uint64(resp[0:8])
	remoteSize := binary.LittleEndian.Uint64(resp[8:16])
	
	if ptr == 0 {
		return nil, fmt.Errorf("rpc: alloc_buffer failed (null pointer)")
	}
	
	return &BufferInfo{
		RemotePtr: ptr,
		Size:      remoteSize,
	}, nil
}

// FreeBuffer frees a previously allocated buffer on the remote server.
// Input format: | remote_ptr (8 bytes) |
func (c *Client) FreeBuffer(remotePtr uint64) error {
	input := make([]byte, 8)
	binary.LittleEndian.PutUint64(input, remotePtr)
	
	_, err := c.sendCommand(CmdFreeBuffer, input)
	return err
}

// BufferClear zeros out a buffer on the remote server.
// Input format: | rpc_tensor | offset (8 bytes) | size (8 bytes) |
func (c *Client) BufferClear(remotePtr uint64, offset, size uint64) error {
	input := make([]byte, 24)
	binary.LittleEndian.PutUint64(input[0:8], remotePtr)
	binary.LittleEndian.PutUint64(input[8:16], offset)
	binary.LittleEndian.PutUint64(input[16:24], size)
	
	_, err := c.sendCommand(CmdBufferClear, input)
	return err
}

// SetTensor sends tensor data to the remote server.
// Input format: | rpc_tensor | offset (8 bytes) | data (size bytes) |
func (c *Client) SetTensor(tensor *RPCTensor, offset uint64, data []byte) error {
	// Serialize the tensor header
	tensorBuf, err := serializeRPCTensor(tensor)
	if err != nil {
		return fmt.Errorf("rpc: serialize tensor: %w", err)
	}
	
	input := make([]byte, 0, len(tensorBuf)+8+len(data))
	input = append(input, tensorBuf...)
	offsetBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(offsetBuf, offset)
	input = append(input, offsetBuf...)
	input = append(input, data...)
	
	_, err = c.sendCommand(CmdSetTensor, input)
	return err
}

// GetTensor retrieves tensor data from the remote server.
// Input format: | rpc_tensor | offset (8 bytes) | size (8 bytes) |
// Response: the tensor data bytes.
func (c *Client) GetTensor(tensor *RPCTensor, offset, size uint64) ([]byte, error) {
	tensorBuf, err := serializeRPCTensor(tensor)
	if err != nil {
		return nil, fmt.Errorf("rpc: serialize tensor: %w", err)
	}
	
	input := make([]byte, 0, len(tensorBuf)+16)
	input = append(input, tensorBuf...)
	offsetBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(offsetBuf, offset)
	input = append(input, offsetBuf...)
	sizeBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(sizeBuf, size)
	input = append(input, sizeBuf...)
	
	return c.sendCommand(CmdGetTensor, input)
}

// GraphCompute sends a computation graph to the remote server.
// Input format: | n_nodes (4 bytes) | nodes (n_nodes * 8 bytes) | n_tensors (4 bytes) | tensors (n_tensors * sizeof(rpc_tensor)) |
// Response: 1 byte status code (ggml_status)
func (c *Client) GraphCompute(input []byte) (byte, error) {
	resp, err := c.sendCommand(CmdGraphCompute, input)
	if err != nil {
		return 255, err
	}
	if len(resp) < 1 {
		return 255, fmt.Errorf("rpc: graph_compute response too short")
	}
	return resp[0], nil
}

// serializeRPCTensor serializes an RPCTensor struct to its binary wire format.
func serializeRPCTensor(t *RPCTensor) ([]byte, error) {
	// Calculate total struct size matching the C++ layout
	// The C++ struct has specific padding; we serialize field by field
	buf := make([]byte, 0, 268) // approximate size
	
	// ID (8)
	idBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(idBuf, t.ID)
	buf = append(buf, idBuf...)
	
	// Type (4)
	typeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(typeBuf, t.Type)
	buf = append(buf, typeBuf...)
	
	// Buffer (8)
	buf64 := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf64, t.Buffer)
	buf = append(buf, buf64...)
	
	// NE (4 * uint32 = 16)
	for _, v := range t.NE {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		buf = append(buf, b...)
	}
	
	// NB (4 * uint32 = 16)
	for _, v := range t.NB {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		buf = append(buf, b...)
	}
	
	// Op (4)
	opBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(opBuf, t.Op)
	buf = append(buf, opBuf...)
	
	// OpParams (16 * int32 = 64)
	for _, v := range t.OpParams {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(v))
		buf = append(buf, b...)
	}
	
	// Flags (4)
	flagsBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(flagsBuf, uint32(t.Flags))
	buf = append(buf, flagsBuf...)
	
	// Src (6 * uint64 = 48)
	for _, v := range t.Src {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, v)
		buf = append(buf, b...)
	}
	
	// ViewSrc (8)
	vsBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(vsBuf, t.ViewSrc)
	buf = append(buf, vsBuf...)
	
	// ViewOffs (8)
	voBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(voBuf, t.ViewOffs)
	buf = append(buf, voBuf...)
	
	// Data (8)
	dataBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(dataBuf, t.Data)
	buf = append(buf, dataBuf...)
	
	// Name (64 bytes, null-padded)
	nameBuf := make([]byte, 64)
	copy(nameBuf, t.Name[:])
	buf = append(buf, nameBuf...)
	
	// Padding (4 bytes)
	padBuf := make([]byte, 4)
	copy(padBuf, t.Padding[:])
	buf = append(buf, padBuf...)
	
	return buf, nil
}

// Addr returns the address of the RPC server.
func (c *Client) Addr() string {
	return c.addr
}
