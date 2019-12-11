package main

import (
	"bytes"
	"encoding/gob"
	"log"
	"time"
)

// Block represents a block in the blockchain
/*
区块链的存储部分为区块数据，和状态数据, 首先区块链有hash值形成存储上的单项链表。所以区块一旦上链,区块的hash值不能发生变化，否则就会出错.
而Merkle根并不是保证这一安全的。而是用来做SPV的.
*/
type Block struct {
	Timestamp    int64 //由矿工出块是填写.
	Transactions []*Transaction
	//创世区块的PrevBlockHash为0
	PrevBlockHash []byte //在AddBlock中通过get("l")获取最后一个区块hash值填写在这里，并更新当前区块为最后一个区块.
	Hash          []byte //当前区块hash
	Nonce         int    //由矿工挖矿时计算.
	Height        int    //出块时填写.
}

// NewBlock creates and returns Block
func NewBlock(transactions []*Transaction, prevBlockHash []byte, height int) *Block {
	block := &Block{time.Now().Unix(), transactions, prevBlockHash, []byte{}, 0, height}
	pow := NewProofOfWork(block)
	nonce, hash := pow.Run() //挖矿， 这是出块过程.

	block.Hash = hash[:] //所以区块的hash值是挖矿的时候填些上去的，因为nonce的值不一样. 那如果是vrf，区块的hash也要等到共识数据(包括出块人的签名信息)出现后，才能出现，所以也应该是出块人设置hash值,猜测，待确认。
	block.Nonce = nonce

	return block
}

// NewGenesisBlock creates and returns genesis Block
/*比特币的创世块是挖矿,可能开始的奖励和这里的不一样，所以比特币的所有币都是挖出来的*/
func NewGenesisBlock(coinbase *Transaction) *Block { //创世区块一定是挖矿交易.
	return NewBlock([]*Transaction{coinbase}, []byte{}, 0)
}

// HashTransactions returns a hash of the transactions in the block
func (b *Block) HashTransactions() []byte {
	var transactions [][]byte

	for _, tx := range b.Transactions {
		transactions = append(transactions, tx.Serialize())
	}
	mTree := NewMerkleTree(transactions)

	return mTree.RootNode.Data //返回根节点hash
}

// Serialize serializes the block
func (b *Block) Serialize() []byte {
	var result bytes.Buffer
	encoder := gob.NewEncoder(&result)

	err := encoder.Encode(b)
	if err != nil {
		log.Panic(err)
	}

	return result.Bytes()
}

// DeserializeBlock deserializes a block
func DeserializeBlock(d []byte) *Block {
	var block Block

	decoder := gob.NewDecoder(bytes.NewReader(d))
	err := decoder.Decode(&block)
	if err != nil {
		log.Panic(err)
	}

	return &block
}
