package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/spf13/cobra"
)

type BspVersion int32
type LumpType int32

const (
	LumpEntities     LumpType = 0
	LumpPlanes                = 1
	LumpTextures              = 2
	LumpVertexes              = 3
	LumpVisibility            = 4
	LumpNodes                 = 5
	LumpTexinfo               = 6
	LumpFaces                 = 7
	LumpLighting              = 8
	LumpClipnodes             = 9
	LumpLeafs                 = 10
	LumpMarksurfaces          = 11
	LumpEdges                 = 12
	LumpSurfedges             = 13
	LumpModels                = 14
	LumpTotal                 = 15

	BspVersionStd      BspVersion = 29
	BspVersionHalfLife            = 30
	BspVersion2PSB                = (('2') + ('P' << 8) + ('S' << 16) + ('B' << 24))
	BspVersionBSP2                = (('B') + ('S' << 8) + ('P' << 16) + ('2' << 24))
)

func (b BspVersion) String() string {
	switch b {
	case BspVersionStd:
		return "29"
	case BspVersionHalfLife:
		return "HalfLife"
	case BspVersion2PSB:
		return "2PSB"
	case BspVersionBSP2:
		return "BSP2"
	default:
		return fmt.Sprintf("Unknown version (%d)", int(b))
	}
}

func (l LumpType) String() string {
	switch l {
	case LumpEntities:
		return "Entities"
	case LumpPlanes:
		return "Planes"
	case LumpTextures:
		return "Textures"
	case LumpVertexes:
		return "Vertexes"
	case LumpVisibility:
		return "Visibility"
	case LumpNodes:
		return "Nodes"
	case LumpTexinfo:
		return "Texinfo"
	case LumpFaces:
		return "Faces"
	case LumpLighting:
		return "Lighting"
	case LumpClipnodes:
		return "Clipnodes"
	case LumpLeafs:
		return "Leafs"
	case LumpMarksurfaces:
		return "Marksurfaces"
	case LumpEdges:
		return "Edges"
	case LumpSurfedges:
		return "Surfedges"
	case LumpModels:
		return "Models"
	default:
		return fmt.Sprintf("Unknown lump (%d)", int(l))
	}
}

type Lump struct {
	Offset uint32
	Length uint32
}

type BspHeader struct {
	Version BspVersion
	Lumps   [LumpTotal]Lump
}

type BspXHeader struct {
	Id       [4]byte
	NumLumps int32
}

type BspXLump struct {
	LumpName [24]byte
	Offset   uint32
	Length   uint32
}

type Face struct {
	PlaneId   uint16
	Side      uint16
	LedgeId   uint32
	LedgeNum  uint16
	TexinfoId uint16
	TypeLight uint8
	BaseLight uint8
	Light     [2]uint8
	Lightmap  int32
}

type FaceV2 struct {
	PlaneId   uint32
	Side      uint32
	LedgeId   uint32
	LedgeNum  uint32
	TexinfoId uint32
	TypeLight uint8
	BaseLight uint8
	Light     [2]uint8
	Lightmap  int32
}

type Vec4 [4]float32

func (v Vec4) String() string {
	return fmt.Sprintf("{x: %.3f, y: %.3f, z: %.3f, w: %.3f}", v[0], v[1], v[2], v[3])
}

type DecoupledLM struct {
	LmWidth        uint16
	LmHeight       uint16
	Offset         int32
	WorldToLmSpace [2]Vec4
}

func (d DecoupledLM) String() string {
	return fmt.Sprintf("LM[w: %2d, h: %2d, off: %6d, [%s, %s]", d.LmWidth, d.LmHeight, d.Offset, d.WorldToLmSpace[0], d.WorldToLmSpace[1])
}

const BspXLumpHeaderSize = 24 + 4 + 4

type BspFile struct {
	BspHeader  BspHeader
	BspXOffset int64
	BspXHeader BspXHeader
	BspXLumps  []BspXLump
}

func BytesToString(buffer []byte) string {
	return fmt.Sprintf("%s", bytes.Trim(buffer, "\x00"))
}

func ReadBspFile(f *os.File) BspFile {
	var bspFile BspFile

	err := binary.Read(f, binary.LittleEndian, &bspFile.BspHeader)
	if err != nil {
		panic(err)
	}

	for i := 0; i < LumpTotal; i++ {
		var lump = &bspFile.BspHeader.Lumps[i]
		var end = int64(lump.Offset + lump.Length)
		if end > bspFile.BspXOffset {
			bspFile.BspXOffset = end
		}
	}

	_, err = f.Seek(bspFile.BspXOffset, os.SEEK_SET)
	if err != nil {
		return bspFile
	}

	err = binary.Read(f, binary.LittleEndian, &bspFile.BspXHeader)
	if err != nil {
		return bspFile
	}

	bspFile.BspXLumps = make([]BspXLump, bspFile.BspXHeader.NumLumps)
	for i := 0; i < len(bspFile.BspXLumps); i++ {
		err = binary.Read(f, binary.LittleEndian, &bspFile.BspXLumps[i])
	}

	return bspFile
}

func WriteBSPX(bspFile *BspFile, f *os.File, destName string, handler func(lumps map[[24]byte][]byte)) {

	out, err := os.Create(destName)
	if err != nil {
		panic(err)
	}
	f.Seek(0, os.SEEK_SET)

	written, err := io.CopyN(out, f, bspFile.BspXOffset)
	if err != nil {
		panic(err)
	}

	if written != bspFile.BspXOffset {
		panic("Could not write new map")
	}

	bspx := map[[24]byte][]byte{}
	for _, xlump := range bspFile.BspXLumps {
		var buffer = make([]byte, xlump.Length)
		f.Seek(int64(xlump.Offset), os.SEEK_SET)
		f.Read(buffer)
		bspx[xlump.LumpName] = buffer
	}

	handler(bspx)

	binary.Write(out, binary.LittleEndian, bspFile.BspXHeader.Id)
	binary.Write(out, binary.LittleEndian, int32(len(bspx)))

	offset, err := out.Seek(0, os.SEEK_CUR)
	if err != nil {
		panic(err)
	}

	offset += int64(BspXLumpHeaderSize * len(bspx))

	for lumpName, buffer := range bspx {
		xlump := BspXLump{
			LumpName: lumpName,
			Offset:   uint32(offset),
			Length:   uint32(len(buffer)),
		}
		offset += int64(xlump.Length)
		binary.Write(out, binary.LittleEndian, xlump)
	}

	for _, buffer := range bspx {
		out.Write(buffer)
	}

	err = out.Sync()
	if err != nil {
		panic(err)
	}

	err = out.Close()
	if err != nil {
		panic(err)
	}
}

func PrintDecoupledLM(bspFile *BspFile, f *os.File) error {
	var numFaces int
	switch bspFile.BspHeader.Version {
	case BspVersionStd:
		numFaces = int(bspFile.BspHeader.Lumps[LumpFaces].Length / uint32(unsafe.Sizeof(Face{})))
		break
	case BspVersionBSP2:
		numFaces = int(bspFile.BspHeader.Lumps[LumpFaces].Length / uint32(unsafe.Sizeof(FaceV2{})))
		break
	default:
		fmt.Printf("Detailed print of BSP version %s not supported\n", bspFile.BspHeader.Version)
		break
	}
	for i := 0; i < len(bspFile.BspXLumps); i++ {
		if BytesToString(bspFile.BspXLumps[i].LumpName[:]) != "DECOUPLED_LM" {
			continue
		}
		_, err := f.Seek(int64(bspFile.BspXLumps[i].Offset), io.SeekStart)
		if err != nil {
			return err
		}
		for j := 0; j < numFaces; j++ {
			var Lightmap DecoupledLM
			err := binary.Read(f, binary.LittleEndian, &Lightmap)
			if err != nil {
				return err
			}
			fmt.Printf("%s\n", Lightmap)
		}
	}
	return nil
}

var printCmd = &cobra.Command{
	Use:   "print <map>",
	Short: "Print BSP structure",
	Long:  `Print the full list of both BSP and BSPX lumps`,
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		f, err := os.Open(args[len(args)-1])
		if err != nil {
			panic(err)
		}
		defer f.Close()

		fmt.Println(args[len(args)-1])

		bspFile := ReadBspFile(f)
		if len(args) > 1 {
			if args[0] == "DECOUPLED_LM" {
				PrintDecoupledLM(&bspFile, f)
			} else {
				fmt.Printf("Detailed print of %s not supported\n", args[1])
			}
		} else {
			fmt.Println("Filename:", path.Base(args[0]))
			fmt.Println(" Version:", bspFile.BspHeader.Version)
			fmt.Println("   Lumps:")

			for i, lump := range bspFile.BspHeader.Lumps {
				fmt.Printf("     %-24s %8.1f kB @ %8d ofs\n", LumpType(i), float64(lump.Length)/1024.0, lump.Offset)
			}

			if len(bspFile.BspXLumps) > 0 {
				fmt.Printf("  XLumps:                                 @ %8d ofs\n", bspFile.BspXOffset)

				for _, xlump := range bspFile.BspXLumps {
					fmt.Printf("     %-24s %8.1f kB @ %8d ofs\n", BytesToString(xlump.LumpName[:]), float64(xlump.Length)/1024, xlump.Offset)
				}
			}

			fmt.Println("")
		}
	},
}

var setLumpCmd = &cobra.Command{
	Use:   "set <map> <lump-name> <path-to-data>",
	Short: "Add or update content of a BSPX lump",
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		f, err := os.Open(args[0])
		if err != nil {
			panic(err)
		}
		defer f.Close()

		var lumpNameRaw [24]byte
		copy(lumpNameRaw[:], []byte(args[1]))

		buffer, err := os.ReadFile(args[2])
		if err != nil {
			panic(err)
		}

		basename := strings.TrimSuffix(args[0], filepath.Ext(args[0]))
		bspFile := ReadBspFile(f)
		WriteBSPX(&bspFile, f, fmt.Sprintf("%s.new.bsp", basename), func(lumps map[[24]byte][]byte) {
			lumps[lumpNameRaw] = buffer
		})
	},
}

var unsetLumpCmd = &cobra.Command{
	Use:   "unset <map> <lump-name>",
	Short: "Removes a BSPX lump",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		f, err := os.Open(args[0])
		if err != nil {
			panic(err)
		}
		defer f.Close()

		var lumpNameRaw [24]byte
		copy(lumpNameRaw[:], []byte(args[1]))

		basename := strings.TrimSuffix(args[0], filepath.Ext(args[0]))
		bspFile := ReadBspFile(f)
		WriteBSPX(&bspFile, f, fmt.Sprintf("%s.new.bsp", basename), func(lumps map[[24]byte][]byte) {
			delete(lumps, lumpNameRaw)
		})
	},
}

var animSuffixCache = map[string]string{}

func randomLetters(n int) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func obfuscateTextureName(original string) string {
	trimmed := strings.TrimRight(original, "\x00 ")

	const totalLen = 15

	//----------------------------------------------------------------
	// 1) Animated textures: +0foo, +1foo, +2foo, +afoo, etc.
	//----------------------------------------------------------------
	if strings.HasPrefix(trimmed, "+") && len(trimmed) > 1 {
		prefix := trimmed[:2]
		suffix := trimmed[2:]

		prefixLen := len(prefix)
		suffixLen := totalLen - prefixLen

		if suffixLen < 0 {
			return prefix[:totalLen]
		}

		scrambledSuffix, found := animSuffixCache[suffix]
		if !found {
			scrambledSuffix = randomLetters(suffixLen)
			animSuffixCache[suffix] = scrambledSuffix
		} else {
			if len(scrambledSuffix) != suffixLen {
				scrambledSuffix = randomLetters(suffixLen)
				animSuffixCache[suffix] = scrambledSuffix
			}
		}

		return prefix + scrambledSuffix
	}

	liquidPrefixes := []string{"*water", "*lava", "*slime", "*tele"}
	for _, lp := range liquidPrefixes {
		if strings.HasPrefix(trimmed, lp) {
			return preserveAndScrambleFixed(lp, trimmed, totalLen)
		}
	}

	if strings.HasPrefix(trimmed, "*") {
		return preserveAndScrambleFixed("*", trimmed, totalLen)
	}

	if strings.HasPrefix(trimmed, "{") {
		return preserveAndScrambleFixed("{", trimmed, totalLen)
	}

	if strings.HasPrefix(trimmed, "sky") {
		return preserveAndScrambleFixed("sky", trimmed, totalLen)
	}

	return randomLetters(totalLen)
}

func preserveAndScrambleFixed(prefix, original string, totalLen int) string {
	prefixLen := len(prefix)
	if prefixLen >= totalLen {
		return prefix[:totalLen]
	}
	scrambleLen := totalLen - prefixLen
	return prefix + randomLetters(scrambleLen)
}

var obfuscateTextureNamesCmd = &cobra.Command{
	Use:   "obfuscate <map>",
	Short: "Randomizes texture names",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		f, err := os.Open(args[0])
		if err != nil {
			panic(err)
		}
		defer f.Close()

		basename := strings.TrimSuffix(args[0], filepath.Ext(args[0]))
		destname := fmt.Sprintf("%s.new.bsp", basename)

		destFile, err := os.Create(destname)
		if err != nil {
			panic(err)
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, f); err != nil {
			panic(err)
		}

		if err := destFile.Sync(); err != nil {
			panic(err)
		}

		rand.Seed(time.Now().UnixNano())

		destFile.Seek(0, io.SeekStart)
		bspFile := ReadBspFile(destFile)

		destFile.Seek(int64(bspFile.BspHeader.Lumps[LumpTextures].Offset), io.SeekStart)
		var numMips uint32
		err = binary.Read(destFile, binary.LittleEndian, &numMips)
		if err != nil {
			panic(err)
		}
		fmt.Println(numMips)
		var offsets = make([]uint32, numMips)
		err = binary.Read(destFile, binary.LittleEndian, &offsets)
		if err != nil {
			panic(err)
		}

		for _, offset := range offsets {
			if offset == math.MaxUint32 {
				continue
			}
			destFile.Seek(int64(bspFile.BspHeader.Lumps[LumpTextures].Offset+offset), io.SeekStart)
			var rawName [16]byte
			err = binary.Read(destFile, binary.LittleEndian, &rawName)
			if err != nil {
				panic(err)
			}

			name := string(rawName[:])
			obf := obfuscateTextureName(name)

			fmt.Println(name + " => " + obf)

			var name16 [15]byte
			copy(name16[:], obf) // copies up to 15 bytes

			destFile.Seek(int64(bspFile.BspHeader.Lumps[LumpTextures].Offset+offset), io.SeekStart)
			err = binary.Write(destFile, binary.LittleEndian, name16)
			if err != nil {
				panic(err)
			}
		}

		err = destFile.Sync()
		if err != nil {
			panic(err)
		}
	},
}

var rootCmd = &cobra.Command{
	Use:   "bspxmgr",
	Short: `bspxmgr manages BPS stuff.`,
	Long:  `bspxmgr handles adding, removing, and updating BSPX assets, and obfuscates texture names.`,
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(printCmd)
	rootCmd.AddCommand(setLumpCmd)
	rootCmd.AddCommand(unsetLumpCmd)
	rootCmd.AddCommand(obfuscateTextureNamesCmd)
}
