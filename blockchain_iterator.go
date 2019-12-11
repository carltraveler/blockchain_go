package main

import (
	"log"

	"github.com/boltdb/bolt"
)

// BlockchainIterator is used to iterate over blockchain blocks
type BlockchainIterator struct {
	currentHash []byte //currentHash是Block的hash值. 记录迭代器上一次迭代的位置。及区块链迭代器区块游标.
	db          *bolt.DB
}

// Next returns next block starting from the tip
//遍历的流程是从最新的区块到创世块的过程.
func (i *BlockchainIterator) Next() *Block {
	var block *Block

	err := i.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		encodedBlock := b.Get(i.currentHash) //根据block的hash值获取block.
		block = DeserializeBlock(encodedBlock)

		return nil
	})

	if err != nil {
		log.Panic(err)
	}

	i.currentHash = block.PrevBlockHash //下一次调用，会根据i.currentHash返回前一个区块的hash

	return block
}
