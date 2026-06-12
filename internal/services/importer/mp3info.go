package importer

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// mp3AudioInfo returns the average bitrate (kbps) and duration (ms) of an MP3
// by parsing the first MPEG audio frame header. VBR files are detected via
// the Xing/Info header (frame count -> exact duration -> average bitrate);
// CBR files use the header bitrate and the audio byte count.
//
// Imported tracks need a real bitrate so the quality-upgrade scanner ranks
// them against the configured tiers instead of treating them as unknown.
func mp3AudioInfo(path string) (bitrateKbps, durationMs int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	audioBytes := st.Size()

	// Skip an ID3v2 tag if present: "ID3" + version(2) + flags(1) + syncsafe size(4).
	head := make([]byte, 10)
	if _, err := io.ReadFull(f, head); err != nil {
		return 0, 0, err
	}
	var offset int64
	if string(head[0:3]) == "ID3" {
		size := int64(head[6]&0x7F)<<21 | int64(head[7]&0x7F)<<14 | int64(head[8]&0x7F)<<7 | int64(head[9]&0x7F)
		offset = 10 + size
	}
	audioBytes -= offset

	// An ID3v1 tag is a fixed 128 bytes at the end.
	if st.Size() >= 128 {
		tail := make([]byte, 3)
		if _, err := f.ReadAt(tail, st.Size()-128); err == nil && string(tail) == "TAG" {
			audioBytes -= 128
		}
	}
	if audioBytes <= 0 {
		return 0, 0, fmt.Errorf("no audio data")
	}

	// Find the first frame sync within a bounded window.
	buf := make([]byte, 64*1024)
	n, _ := f.ReadAt(buf, offset)
	buf = buf[:n]

	var hdr uint32
	var hdrPos int
	found := false
	for i := 0; i+4 <= len(buf); i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		hdr = binary.BigEndian.Uint32(buf[i : i+4])
		if validFrameHeader(hdr) {
			hdrPos = i
			found = true
			break
		}
	}
	if !found {
		return 0, 0, fmt.Errorf("no MPEG frame sync found")
	}

	version := (hdr >> 19) & 0x3 // 0=2.5, 2=2, 3=1
	layer := (hdr >> 17) & 0x3   // 1=III, 2=II, 3=I
	bitrateIdx := (hdr >> 12) & 0xF
	sampleIdx := (hdr >> 10) & 0x3
	channelMode := (hdr >> 6) & 0x3

	sampleRate := sampleRates[version][sampleIdx]
	headerBitrate := bitrates[version][layer][bitrateIdx]
	if sampleRate == 0 || headerBitrate == 0 {
		return 0, 0, fmt.Errorf("unsupported frame header")
	}

	samplesPerFrame := 1152 // Layer III, MPEG1
	if layer == 1 && version != 3 {
		samplesPerFrame = 576 // Layer III, MPEG2/2.5
	}

	// Xing/Info header (VBR or VBR-prepared CBR): exact frame count.
	sideInfo := 32 // MPEG1, stereo-ish modes
	if version == 3 {
		if channelMode == 3 {
			sideInfo = 17 // MPEG1 mono
		}
	} else {
		sideInfo = 17 // MPEG2/2.5 stereo-ish
		if channelMode == 3 {
			sideInfo = 9 // MPEG2/2.5 mono
		}
	}
	xingPos := hdrPos + 4 + sideInfo
	if xingPos+16 <= len(buf) {
		tagName := string(buf[xingPos : xingPos+4])
		if tagName == "Xing" || tagName == "Info" {
			flags := binary.BigEndian.Uint32(buf[xingPos+4 : xingPos+8])
			if flags&0x1 != 0 { // frames field present
				frames := binary.BigEndian.Uint32(buf[xingPos+8 : xingPos+12])
				if frames > 0 {
					durationMs = int(int64(frames) * int64(samplesPerFrame) * 1000 / int64(sampleRate))
					if durationMs > 0 {
						bitrateKbps = int(audioBytes * 8 / int64(durationMs)) // bytes*8/ms = kbps
						return bitrateKbps, durationMs, nil
					}
				}
			}
		}
	}

	// CBR: trust the header bitrate.
	bitrateKbps = headerBitrate
	durationMs = int(audioBytes * 8 / int64(bitrateKbps))
	return bitrateKbps, durationMs, nil
}

func validFrameHeader(hdr uint32) bool {
	version := (hdr >> 19) & 0x3
	layer := (hdr >> 17) & 0x3
	bitrateIdx := (hdr >> 12) & 0xF
	sampleIdx := (hdr >> 10) & 0x3
	if version == 1 || layer == 0 { // reserved values
		return false
	}
	if bitrateIdx == 0 || bitrateIdx == 0xF { // free-format or invalid
		return false
	}
	if sampleIdx == 3 {
		return false
	}
	return bitrates[version][layer][bitrateIdx] != 0 && sampleRates[version][sampleIdx] != 0
}

// bitrates[version][layer][index] in kbps. Version: 0=MPEG2.5, 2=MPEG2,
// 3=MPEG1. Layer: 1=III, 2=II, 3=I. Zero = invalid combination.
var bitrates = map[uint32]map[uint32][16]int{
	3: { // MPEG1
		3: {0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0}, // Layer I
		2: {0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0},    // Layer II
		1: {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},     // Layer III
	},
	2: { // MPEG2
		3: {0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0},
		2: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
		1: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	},
	0: { // MPEG2.5
		3: {0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0},
		2: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
		1: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	},
}

// sampleRates[version][index] in Hz.
var sampleRates = map[uint32][4]int{
	3: {44100, 48000, 32000, 0}, // MPEG1
	2: {22050, 24000, 16000, 0}, // MPEG2
	0: {11025, 12000, 8000, 0},  // MPEG2.5
}
