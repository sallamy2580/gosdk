package sdk

import (
	"bytes"
	"encoding/hex"
	"io"
	"math"
	"math/bits"
	"os"
	"sync"

	"github.com/0chain/gosdk/zboxcore/encoder"
	"github.com/0chain/gosdk/zboxcore/fileref"
	. "github.com/0chain/gosdk/zboxcore/logger"
)

func (req *UploadRequest) pushThumbnailData(data []byte) error {
	//TODO: Check for optimization
	n := int64(math.Min(float64(req.thumbRemaining), float64(len(data))))
	if !req.isRepair {
		req.thumbnailHashWr.Write(data[:n])
	}
	req.thumbRemaining = req.thumbRemaining - n
	erasureencoder, err := encoder.NewEncoder(req.datashards, req.parityshards)
	if err != nil {
		return err
	}
	shards, err := erasureencoder.Encode(data)
	if err != nil {
		Logger.Error("Erasure coding failed.", err.Error())
		return err
	}
	c, pos := 0, 0
	for i := req.uploadMask; i != 0; i &= ^(1 << uint32(pos)) {
		pos = bits.TrailingZeros32(i)
		req.uploadThumbCh[c] <- shards[pos]
		c++
	}
	return nil
}

func (req *UploadRequest) processThumbnail(a *Allocation, wg *sync.WaitGroup) {
	defer wg.Done()
	var inFile *os.File
	inFile, err := os.Open(req.thumbnailpath)
	if err != nil {
		return
	}
	size := req.filemeta.ThumbnailSize
	// Calculate number of bytes per shard.
	perShard := (size + int64(a.DataShards) - 1) / int64(a.DataShards)
	// Pad data to Shards*perShard.
	padding := make([]byte, (int64(a.DataShards)*perShard)-size)
	dataReader := io.MultiReader(inFile, bytes.NewBuffer(padding))
	chunksPerShard := (perShard + int64(fileref.CHUNK_SIZE) - 1) / fileref.CHUNK_SIZE
	Logger.Debug("Thumbnail Size:", size, " perShard:", perShard, " chunks/shard:", chunksPerShard)

	sent := int(0)
	for ctr := int64(0); ctr < chunksPerShard; ctr++ {
		remaining := int64(math.Min(float64(perShard-(ctr*fileref.CHUNK_SIZE)), fileref.CHUNK_SIZE))
		b1 := make([]byte, remaining*int64(a.DataShards))
		_, err = dataReader.Read(b1)
		if err != nil {
			return
		}
		err = req.pushThumbnailData(b1)
		if err != nil {
			return
		}
		sent = sent + int(remaining*int64(a.DataShards+a.ParityShards))
	}
	err = req.completeThumbnailPush()
	if err != nil {
		return
	}
}

func (req *UploadRequest) completeThumbnailPush() error {
	if !req.isRepair {
		req.filemeta.ThumbnailHash = hex.EncodeToString(req.thumbnailHash.Sum(nil))
		c, pos := 0, 0
		for i := req.uploadMask; i != 0; i &= ^(1 << uint32(pos)) {
			pos = bits.TrailingZeros32(i)
			req.uploadThumbCh[c] <- []byte("done")
			c++
		}
	}
	return nil
}