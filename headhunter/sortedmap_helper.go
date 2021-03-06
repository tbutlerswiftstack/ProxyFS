package headhunter

import (
	"fmt"

	"github.com/swiftstack/ProxyFS/swiftclient"
	"github.com/swiftstack/ProxyFS/utils"
	"github.com/swiftstack/sortedmap"
)

func (bPlusTreeWrapper *bPlusTreeWrapperStruct) DumpKey(key sortedmap.Key) (keyAsString string, err error) {
	keyAsUint64, ok := key.(uint64)
	if !ok {
		err = fmt.Errorf("headhunter.bPlusTreeWrapper.DumpKey() could not parse key as a uint64")
		return
	}

	keyAsString = fmt.Sprintf("0x%016X", keyAsUint64)

	err = nil
	return
}

func (bPlusTreeWrapper *bPlusTreeWrapperStruct) DumpValue(value sortedmap.Value) (valueAsString string, err error) {
	valueAsByteSlice, ok := value.([]byte)
	if !ok {
		err = fmt.Errorf("headhunter.bPlusTreeWrapper.DumpValue() count not parse value as []byte")
		return
	}

	if 0 == len(valueAsByteSlice) {
		valueAsString = ""
		err = nil
		return
	}

	valueAsHexDigitByteSlice := make([]byte, 2+(3*(len(valueAsByteSlice)-1)))

	for i, u8 := range valueAsByteSlice {
		if i == 0 {
			valueAsHexDigitByteSlice[0] = utils.ByteToHexDigit(u8 >> 4)
			valueAsHexDigitByteSlice[1] = utils.ByteToHexDigit(u8 & 0x0F)
		} else {
			valueAsHexDigitByteSlice[(3*i)-1] = ' '
			valueAsHexDigitByteSlice[(3*i)+0] = utils.ByteToHexDigit(u8 >> 4)
			valueAsHexDigitByteSlice[(3*i)+1] = utils.ByteToHexDigit(u8 & 0x0F)
		}
	}

	valueAsString = string(valueAsHexDigitByteSlice[:])

	err = nil
	return
}

func (bPlusTreeWrapper *bPlusTreeWrapperStruct) GetNode(objectNumber uint64, objectOffset uint64, objectLength uint64) (nodeByteSlice []byte, err error) {
	nodeByteSlice, err =
		swiftclient.ObjectGet(
			bPlusTreeWrapper.volume.accountName,
			bPlusTreeWrapper.volume.checkpointContainerName,
			utils.Uint64ToHexStr(objectNumber),
			objectOffset,
			objectLength)
	return
}

func (bPlusTreeWrapper *bPlusTreeWrapperStruct) PutNode(nodeByteSlice []byte) (objectNumber uint64, objectOffset uint64, err error) {
	var (
		bytesUsed uint64
		ok        bool
	)

	err = bPlusTreeWrapper.volume.openCheckpointChunkedPutContextIfNecessary()
	if nil != err {
		return
	}

	objectNumber = bPlusTreeWrapper.volume.checkpointHeader.CheckpointObjectTrailerV2StructObjectNumber

	objectOffset, err = bPlusTreeWrapper.volume.bytesPutToCheckpointChunkedPutContext()
	if nil != err {
		return
	}

	err = bPlusTreeWrapper.volume.sendChunkToCheckpointChunkedPutContext(nodeByteSlice)
	if nil != err {
		return
	}

	bPlusTreeWrapper.volume.checkpointFlushedData = true

	switch bPlusTreeWrapper.wrapperType {
	case inodeRecBPlusTreeWrapperType:
		bytesUsed, ok = bPlusTreeWrapper.volume.inodeRecBPlusTreeLayout[objectNumber]
		if ok {
			bPlusTreeWrapper.volume.inodeRecBPlusTreeLayout[objectNumber] = bytesUsed + uint64(len(nodeByteSlice))
		} else {
			bPlusTreeWrapper.volume.inodeRecBPlusTreeLayout[objectNumber] = uint64(len(nodeByteSlice))
		}
	case logSegmentRecBPlusTreeWrapperType:
		bytesUsed, ok = bPlusTreeWrapper.volume.logSegmentRecBPlusTreeLayout[objectNumber]
		if ok {
			bPlusTreeWrapper.volume.logSegmentRecBPlusTreeLayout[objectNumber] = bytesUsed + uint64(len(nodeByteSlice))
		} else {
			bPlusTreeWrapper.volume.logSegmentRecBPlusTreeLayout[objectNumber] = uint64(len(nodeByteSlice))
		}
	case bPlusTreeObjectBPlusTreeWrapperType:
		bytesUsed, ok = bPlusTreeWrapper.volume.bPlusTreeObjectBPlusTreeLayout[objectNumber]
		if ok {
			bPlusTreeWrapper.volume.bPlusTreeObjectBPlusTreeLayout[objectNumber] = bytesUsed + uint64(len(nodeByteSlice))
		} else {
			bPlusTreeWrapper.volume.bPlusTreeObjectBPlusTreeLayout[objectNumber] = uint64(len(nodeByteSlice))
		}
	default:
		err = fmt.Errorf("Logic error: bPlusTreeWrapper.PutNode() called for invalid wrapperType: %v", bPlusTreeWrapper.wrapperType)
		panic(err)
	}

	err = bPlusTreeWrapper.volume.closeCheckpointChunkedPutContextIfNecessary()

	return // err set as appropriate
}

func (bPlusTreeWrapper *bPlusTreeWrapperStruct) DiscardNode(objectNumber uint64, objectOffset uint64, objectLength uint64) (err error) {
	var (
		bytesUsed uint64
		ok        bool
	)

	switch bPlusTreeWrapper.wrapperType {
	case inodeRecBPlusTreeWrapperType:
		bytesUsed, ok = bPlusTreeWrapper.volume.inodeRecBPlusTreeLayout[objectNumber]
		if ok {
			if bytesUsed < objectLength {
				err = fmt.Errorf("Logic error: [inodeRecBPlusTreeWrapperType] bPlusTreeWrapper.DiscardNode() called to dereference too many bytes in objectNumber 0x%016X", objectNumber)
			} else {
				err = nil
				bPlusTreeWrapper.volume.inodeRecBPlusTreeLayout[objectNumber] = bytesUsed - objectLength
			}
		} else {
			err = fmt.Errorf("Logic error: [inodeRecBPlusTreeWrapperType] bPlusTreeWrapper.DiscardNode() called referencing invalid objectNumber: 0x%016X", objectNumber)
		}
	case logSegmentRecBPlusTreeWrapperType:
		bytesUsed, ok = bPlusTreeWrapper.volume.logSegmentRecBPlusTreeLayout[objectNumber]
		if ok {
			if bytesUsed < objectLength {
				err = fmt.Errorf("Logic error: [logSegmentRecBPlusTreeWrapperType] bPlusTreeWrapper.DiscardNode() called to dereference too many bytes in objectNumber 0x%016X", objectNumber)
			} else {
				err = nil
				bPlusTreeWrapper.volume.logSegmentRecBPlusTreeLayout[objectNumber] = bytesUsed - objectLength
			}
		} else {
			err = fmt.Errorf("Logic error: [logSegmentRecBPlusTreeWrapperType] bPlusTreeWrapper.DiscardNode() called referencing invalid objectNumber: 0x%016X", objectNumber)
		}
	case bPlusTreeObjectBPlusTreeWrapperType:
		bytesUsed, ok = bPlusTreeWrapper.volume.bPlusTreeObjectBPlusTreeLayout[objectNumber]
		if ok {
			if bytesUsed < objectLength {
				err = fmt.Errorf("Logic error: [bPlusTreeObjectBPlusTreeWrapperType] bPlusTreeWrapper.DiscardNode() called to dereference too many bytes in objectNumber 0x%016X", objectNumber)
			} else {
				err = nil
				bPlusTreeWrapper.volume.bPlusTreeObjectBPlusTreeLayout[objectNumber] = bytesUsed - objectLength
			}
		} else {
			err = fmt.Errorf("Logic error: [bPlusTreeObjectBPlusTreeWrapperType] bPlusTreeWrapper.DiscardNode() called referencing invalid objectNumber: 0x%016X", objectNumber)
		}
	default:
		err = fmt.Errorf("Logic error: bPlusTreeWrapper.DiscardNode() called for invalid wrapperType: %v", bPlusTreeWrapper.wrapperType)
	}

	return // err set as appropriate regardless of path
}

func (bPlusTreeWrapper *bPlusTreeWrapperStruct) PackKey(key sortedmap.Key) (packedKey []byte, err error) {
	keyAsUint64, ok := key.(uint64)
	if !ok {
		err = fmt.Errorf("*bPlusTreeWrapper.PackKey(key == %v) failed to convert key to uint64", key)
		return
	}
	packedKey = utils.Uint64ToByteSlice(keyAsUint64)
	err = nil
	return
}

func (bPlusTreeWrapper *bPlusTreeWrapperStruct) UnpackKey(payloadData []byte) (key sortedmap.Key, bytesConsumed uint64, err error) {
	if 8 > len(payloadData) {
		err = fmt.Errorf("*bPlusTreeWrapper.UnpackKey(payloadData) failed - len(payloadData) must be atleast 8 (was %v)", len(payloadData))
		return
	}
	keyAsUint64, ok := utils.ByteSliceToUint64(payloadData[:8])
	if !ok {
		err = fmt.Errorf("*bPlusTreeWrapper.UnpackKey(payloadData) failed in call to utils.ByteSliceToUint64()")
		return
	}
	key = keyAsUint64
	bytesConsumed = 8
	err = nil
	return
}

func (bPlusTreeWrapper *bPlusTreeWrapperStruct) PackValue(value sortedmap.Value) (packedValue []byte, err error) {
	valueAsByteSlice, ok := value.([]byte)
	if !ok {
		err = fmt.Errorf("*bPlusTreeWrapper.PackValue() failed - value isn't a []byte")
		return
	}
	packedValue = utils.Uint64ToByteSlice(uint64(len(valueAsByteSlice)))
	packedValue = append(packedValue, valueAsByteSlice...)
	err = nil
	return
}

func (bPlusTreeWrapper *bPlusTreeWrapperStruct) UnpackValue(payloadData []byte) (value sortedmap.Value, bytesConsumed uint64, err error) {
	if 8 > len(payloadData) {
		err = fmt.Errorf("*bPlusTreeWrapper.UnpackValue(payloadData) failed - len(payloadData) must be atleast 8 (was %v)", len(payloadData))
		return
	}
	valueSize, ok := utils.ByteSliceToUint64(payloadData[:8])
	if !ok {
		err = fmt.Errorf("*bPlusTreeWrapper.UnpackValue(payloadData) failed in call to utils.ByteSliceToUint64()")
		return
	}
	valueAsByteSlice := make([]byte, valueSize)
	if 0 < valueSize {
		copy(valueAsByteSlice, payloadData[8:(8+valueSize)])
	}
	value = valueAsByteSlice
	bytesConsumed = 8 + valueSize
	err = nil
	return
}
