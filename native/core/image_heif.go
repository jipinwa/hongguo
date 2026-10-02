package core

import (
	"encoding/binary"
	"errors"
)

type heifBox struct {
	kind string
	data []byte
}

type heifReader struct {
	data []byte
	err  error
}

func (reader *heifReader) take(size int) []byte {
	if reader.err != nil || size < 0 || size > len(reader.data) {
		reader.err = errors.New("HEIC 数据不完整")
		return nil
	}
	value := reader.data[:size]
	reader.data = reader.data[size:]
	return value
}

func (reader *heifReader) uint(size int) uint64 {
	if size < 0 || size > 8 {
		reader.err = errors.New("HEIC 字段长度无效")
		return 0
	}
	var value uint64
	for _, part := range reader.take(size) {
		value = value<<8 | uint64(part)
	}
	return value
}

func heifBoxes(data []byte) ([]heifBox, error) {
	var boxes []heifBox
	for len(data) > 0 {
		if len(data) < 8 || len(boxes) >= 4096 {
			return nil, errors.New("HEIC 容器结构无效")
		}
		size, header := uint64(binary.BigEndian.Uint32(data)), 8
		if size == 1 {
			if len(data) < 16 {
				return nil, errors.New("HEIC 容器头不完整")
			}
			size, header = binary.BigEndian.Uint64(data[8:]), 16
		} else if size == 0 {
			size = uint64(len(data))
		}
		if size < uint64(header) || size > uint64(len(data)) {
			return nil, errors.New("HEIC 容器长度无效")
		}
		boxes = append(boxes, heifBox{kind: string(data[4:8]), data: data[header:int(size)]})
		data = data[int(size):]
	}
	return boxes, nil
}

func heifBoxData(boxes []heifBox, kind string) []byte {
	for _, box := range boxes {
		if box.kind == kind {
			return box.data
		}
	}
	return nil
}

func isHEICImage(data []byte) bool {
	if len(data) < 16 || string(data[4:8]) != "ftyp" {
		return false
	}
	size := uint64(binary.BigEndian.Uint32(data))
	if size < 16 || size > uint64(len(data)) || size > 256 {
		return false
	}
	for offset := 8; offset+4 <= int(size); offset += 4 {
		if offset == 12 {
			continue
		}
		switch string(data[offset : offset+4]) {
		case "heic", "heix", "hevc", "hevx":
			return true
		}
	}
	return false
}

type heifImage struct {
	data    []byte
	filters []string
}

func extractHEICImage(data []byte) (heifImage, error) {
	var image heifImage
	if len(data) > nativeCoverMaxBytes || !isHEICImage(data) {
		return image, errors.New("不是支持的 HEIC 封面")
	}
	boxes, err := heifBoxes(data)
	if err != nil {
		return image, err
	}
	meta := heifBoxData(boxes, "meta")
	if len(meta) < 4 || meta[0] != 0 {
		return image, errors.New("HEIC 缺少图片元数据")
	}
	children, err := heifBoxes(meta[4:])
	if err != nil {
		return image, err
	}
	primary := &heifReader{data: heifBoxData(children, "pitm")}
	version := primary.uint(1)
	primary.take(3)
	idSize := 2
	if version == 1 {
		idSize = 4
	}
	itemID := primary.uint(idSize)
	if primary.err != nil || version > 1 || itemID == 0 {
		return image, errors.New("HEIC 主图片标识无效")
	}
	if err := validateHEICItem(heifBoxData(children, "iinf"), itemID); err != nil {
		return image, err
	}
	config, filters, err := heifItemProperties(heifBoxData(children, "iprp"), itemID)
	if err != nil {
		return image, err
	}
	item, err := heifItemData(heifBoxData(children, "iloc"), itemID, data, heifBoxData(children, "idat"))
	if err != nil {
		return image, err
	}
	image.data, err = heifAnnexB(config, item)
	image.filters = filters
	return image, err
}

func validateHEICItem(data []byte, itemID uint64) error {
	reader := &heifReader{data: data}
	version := reader.uint(1)
	reader.take(3)
	countSize := 2
	if version == 1 {
		countSize = 4
	}
	count := reader.uint(countSize)
	if reader.err != nil || version > 1 {
		return errors.New("HEIC 图片信息无效")
	}
	boxes, err := heifBoxes(reader.data)
	if err != nil || count != uint64(len(boxes)) {
		return errors.New("HEIC 图片信息不完整")
	}
	for _, box := range boxes {
		if box.kind != "infe" {
			continue
		}
		item := &heifReader{data: box.data}
		version := item.uint(1)
		item.take(3)
		idSize := 2
		if version == 3 {
			idSize = 4
		}
		id := item.uint(idSize)
		protected := item.uint(2)
		kind := string(item.take(4))
		if id == itemID {
			if item.err != nil || (version != 2 && version != 3) || protected != 0 || kind != "hvc1" {
				return errors.New("HEIC 封面不是独立的 HEVC 图片")
			}
			return nil
		}
	}
	return errors.New("HEIC 主图片不存在")
}

func heifItemProperties(data []byte, itemID uint64) ([]byte, []string, error) {
	boxes, err := heifBoxes(data)
	if err != nil {
		return nil, nil, err
	}
	properties, err := heifBoxes(heifBoxData(boxes, "ipco"))
	if err != nil {
		return nil, nil, err
	}
	reader := &heifReader{data: heifBoxData(boxes, "ipma")}
	version, flags := reader.uint(1), reader.uint(3)
	count := reader.uint(4)
	if version > 1 || count > 4096 || reader.err != nil {
		return nil, nil, errors.New("HEIC 图片属性无效")
	}
	var config []byte
	var filters []string
	var width, height uint64
	for entry := uint64(0); entry < count && reader.err == nil; entry++ {
		idSize := 2
		if version == 1 {
			idSize = 4
		}
		id := reader.uint(idSize)
		associations := reader.uint(1)
		for association := uint64(0); association < associations && reader.err == nil; association++ {
			index := reader.uint(1) & 127
			if flags&1 != 0 {
				index = index<<8 | reader.uint(1)
			}
			if id != itemID || index == 0 {
				continue
			}
			if index > uint64(len(properties)) {
				return nil, nil, errors.New("HEIC 属性索引越界")
			}
			property := properties[index-1]
			switch property.kind {
			case "hvcC":
				config = property.data
			case "ispe":
				if len(property.data) != 12 {
					return nil, nil, errors.New("HEIC 图片尺寸无效")
				}
				width = uint64(binary.BigEndian.Uint32(property.data[4:]))
				height = uint64(binary.BigEndian.Uint32(property.data[8:]))
			case "irot", "imir":
				if len(property.data) != 1 {
					return nil, nil, errors.New("HEIC 图片方向无效")
				}
				if property.kind == "imir" {
					if property.data[0]&1 == 0 {
						filters = append(filters, "hflip")
					} else {
						filters = append(filters, "vflip")
					}
				} else {
					switch property.data[0] & 3 {
					case 1:
						filters = append(filters, "transpose=cclock")
					case 2:
						filters = append(filters, "hflip", "vflip")
					case 3:
						filters = append(filters, "transpose=clock")
					}
				}
			}
		}
	}
	if reader.err != nil || len(config) < 23 || width == 0 || height == 0 || width > 4096 || height > 4096 {
		return nil, nil, errors.New("HEIC 编码信息或图片尺寸无效")
	}
	return config, filters, nil
}

func heifItemData(data []byte, itemID uint64, file, inline []byte) ([]byte, error) {
	reader := &heifReader{data: data}
	version := reader.uint(1)
	reader.take(3)
	lengths, offsets := reader.uint(1), reader.uint(1)
	offsetSize, lengthSize := int(lengths>>4), int(lengths&15)
	baseSize, indexSize := int(offsets>>4), 0
	if version > 0 {
		indexSize = int(offsets & 15)
	}
	countSize := 2
	if version == 2 {
		countSize = 4
	}
	count := reader.uint(countSize)
	if reader.err != nil || version > 2 || count > 4096 || offsetSize > 8 || lengthSize > 8 || baseSize > 8 || indexSize > 8 {
		return nil, errors.New("HEIC 图片位置无效")
	}
	var result []byte
	for entry := uint64(0); entry < count && reader.err == nil; entry++ {
		id := reader.uint(countSize)
		method := uint64(0)
		if version > 0 {
			method = reader.uint(2) & 15
		}
		reference, base := reader.uint(2), reader.uint(baseSize)
		extents := reader.uint(2)
		if extents > 4096 || (id == itemID && (reference != 0 || method > 1)) {
			return nil, errors.New("HEIC 不支持外部图片引用")
		}
		for extent := uint64(0); extent < extents && reader.err == nil; extent++ {
			reader.uint(indexSize)
			offset, length := reader.uint(offsetSize), reader.uint(lengthSize)
			if id != itemID || reader.err != nil {
				continue
			}
			source := file
			if method == 1 {
				source = inline
			}
			if base > uint64(len(source)) || offset > uint64(len(source))-base {
				return nil, errors.New("HEIC 图片偏移越界")
			}
			start := base + offset
			if length == 0 {
				length = uint64(len(source)) - start
			}
			if length > uint64(len(source))-start || length > uint64(nativeCoverMaxBytes-len(result)) {
				return nil, errors.New("HEIC 图片长度越界")
			}
			result = append(result, source[start:start+length]...)
		}
	}
	if reader.err != nil || len(result) == 0 {
		return nil, errors.New("HEIC 图片数据缺失")
	}
	return result, nil
}

func heifAnnexB(config, item []byte) ([]byte, error) {
	if len(config) < 23 || config[0] != 1 {
		return nil, errors.New("HEIC 解码配置无效")
	}
	var result []byte
	appendNAL := func(data []byte) error {
		if len(data) < 2 || len(data) > nativeCoverMaxBytes-4-len(result) {
			return errors.New("HEIC 编码数据长度无效")
		}
		result = append(result, 0, 0, 0, 1)
		result = append(result, data...)
		return nil
	}
	reader := &heifReader{data: config[23:]}
	for array := 0; array < int(config[22]) && reader.err == nil; array++ {
		reader.uint(1)
		count := reader.uint(2)
		if count > 4096 {
			return nil, errors.New("HEIC 解码配置过大")
		}
		for index := uint64(0); index < count && reader.err == nil; index++ {
			size := reader.uint(2)
			if err := appendNAL(reader.take(int(size))); err != nil {
				return nil, err
			}
		}
	}
	if reader.err != nil || len(reader.data) != 0 || len(result) == 0 {
		return nil, errors.New("HEIC 解码配置不完整")
	}
	reader = &heifReader{data: item}
	lengthSize := int(config[21]&3) + 1
	for count := 0; len(reader.data) > 0; count++ {
		size := reader.uint(lengthSize)
		if reader.err != nil || size > uint64(len(reader.data)) || count >= 4096 {
			return nil, errors.New("HEIC 图片编码不完整")
		}
		if err := appendNAL(reader.take(int(size))); err != nil {
			return nil, err
		}
	}
	return result, nil
}
