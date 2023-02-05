package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

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
	Offset int32
	Length int32
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
	Offset   int32
	Length   int32
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
			Offset:   int32(offset),
			Length:   int32(len(buffer)),
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

var printCmd = &cobra.Command{
	Use:   "print <map>",
	Short: "Print BSP structure",
	Long:  `Print the full list of both BSP and BSPX lumps`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		f, err := os.Open(args[0])
		if err != nil {
			panic(err)
		}
		defer f.Close()

		bspFile := ReadBspFile(f)

		fmt.Println("Filename:", path.Base(args[0]))
		fmt.Println(" Version:", bspFile.BspHeader.Version)
		fmt.Println("   Lumps:")

		for i, lump := range bspFile.BspHeader.Lumps {
			fmt.Printf("     %-24s %8.1f kB @ %8d ofs\n", LumpType(i), float64(lump.Length)/1024.0, lump.Offset)
		}

		if len(bspFile.BspXLumps) > 0 {
			fmt.Printf("   BSPX:                                  @ %8d ofs\n", bspFile.BspXOffset)

			for _, xlump := range bspFile.BspXLumps {
				fmt.Printf("     %-24s %8.1f kB @ %8d ofs\n", BytesToString(xlump.LumpName[:]), float64(xlump.Length)/1024, xlump.Offset)
			}
		}

		fmt.Println("")
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

var rootCmd = &cobra.Command{
	Use:   "bspxmgr",
	Short: `bspxmgr manages BPSX assets.`,
	Long:  `bspxmgr handles adding, removing, and updating BSPX assets.`,
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
}
