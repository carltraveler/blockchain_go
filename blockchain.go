package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/boltdb/bolt"
)

const dbFile = "blockchain_%s.db" //数据库存储文件，包括所有的链上存储数据. CreateBlockchain是顶层结构，建议这样一个文件.
const blocksBucket = "blocks"     //utxo_set.go中utxoBucket存放的是txid->[]TXOutput数组的映射.即TXOutputs结构体.
const genesisCoinbaseData = "The Times 03/Jan/2009 Chancellor on brink of second bailout for banks"

// Blockchain implements interactions with a DB
type Blockchain struct {
	tip []byte   //记录最后一个区块"l"(表示last)的hash值.
	db  *bolt.DB //Blockchain的utxoBucket的存储对象，k,v存储， k为transaction txid.  v为[]TXOutput数组.
}

// CreateBlockchain creates a new blockchain DB
//The address to send genesis block reward to. address是将创世区块的的奖励
//创造创始交易，由address挖出区块，且奖励是给自己的，挖出创始区块,并写入存储
func CreateBlockchain(address, nodeID string) *Blockchain {
	dbFile := fmt.Sprintf(dbFile, nodeID) //在存储上通过nodeId区分不同的blockchain。 同理，在网络上通过networkid来区分。
	if dbExists(dbFile) {
		fmt.Println("Blockchain already exists.")
		os.Exit(1)
	}

	var tip []byte

	cbtx := NewCoinbaseTX(address, genesisCoinbaseData) //why need data.
	genesis := NewGenesisBlock(cbtx)

	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucket([]byte(blocksBucket))
		if err != nil {
			log.Panic(err)
		}

		err = b.Put(genesis.Hash, genesis.Serialize())
		if err != nil {
			log.Panic(err)
		}

		err = b.Put([]byte("l"), genesis.Hash) //将创始区块写入存储.
		if err != nil {
			log.Panic(err)
		}
		tip = genesis.Hash

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	//所以Blockchain即是记录最后一个区块hash的数据库.
	bc := Blockchain{tip, db} //创造的blockchain的tip是创始区块的hash值.

	return &bc
}

// NewBlockchain creates a new Blockchain with genesis Block
//这里不涉及创始区块的构建. 而是在已有的区块上，构造内存Blockchain概念.
func NewBlockchain(nodeID string) *Blockchain { //在当前数据库基础上(l key是关键)，新建立一个Blockchain
	dbFile := fmt.Sprintf(dbFile, nodeID)
	if dbExists(dbFile) == false {
		fmt.Println("No existing blockchain found. Create one first.")
		os.Exit(1)
	}

	var tip []byte
	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		tip = b.Get([]byte("l")) //查找最后一个区块.l表示获取最后一个区块的hash

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	bc := Blockchain{tip, db}

	return &bc
}

// AddBlock saves the block into the blockchain
func (bc *Blockchain) AddBlock(block *Block) {
	err := bc.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		blockInDb := b.Get(block.Hash)

		if blockInDb != nil { //如果!= nil说明当前区块已经被加入了.
			return nil
		}

		blockData := block.Serialize()      //序列化block
		err := b.Put(block.Hash, blockData) //写入存储 k -> blockData
		if err != nil {
			log.Panic(err)
		}

		lastHash := b.Get([]byte("l"))
		lastBlockData := b.Get(lastHash)
		lastBlock := DeserializeBlock(lastBlockData)

		if block.Height > lastBlock.Height { //如果添加的区块拥有更高的高度，更新'l'
			err = b.Put([]byte("l"), block.Hash)
			if err != nil {
				log.Panic(err)
			}
			bc.tip = block.Hash
		}

		return nil
	})
	if err != nil {
		log.Panic(err)
	}
}

// FindTransaction finds a transaction by its ID
func (bc *Blockchain) FindTransaction(ID []byte) (Transaction, error) {
	bci := bc.Iterator() //遍历Blockchain上的所有transaction. 所以需要两层遍历，遍历block, 遍历block中的transaction.

	for {
		block := bci.Next()

		for _, tx := range block.Transactions {
			if bytes.Compare(tx.ID, ID) == 0 { //遍历所有Block, 遍历所有Transaction, 找到ID对应的Transaction.
				return *tx, nil //找到则返回.
			}
		}

		if len(block.PrevBlockHash) == 0 {
			break
		}
	}

	return Transaction{}, errors.New("Transaction is not found")
}

// FindUTXO finds all unspent transaction outputs and returns transactions with spent outputs removed
// 找出所有区块中，未被花费的交易输出，即UTXO.
//FindUTXO有两个函数， 一个是UTXO set的成员函数，一个是Blockchain的.
//block为height的区块中的tx,只被被下一个区块引用, 如果从最后一个区块反向遍历到height区块, 就能找到height区块中教的的TXOutput,所有被引用的TXOutput,剩下的就是UTXO. 所以从最新区块向上遍历，tx.TXInput能不断被发现被消耗的UTXO.
//block内的区块的transaction之间不会发生引用. 因为还没有落账.
func (bc *Blockchain) FindUTXO() map[string]TXOutputs { //返回UTXO set. 以key(txID) ===> TXOutputs
	UTXO := make(map[string]TXOutputs)
	spentTXOs := make(map[string][]int) // txID->voutindex的映射， 表明txID的TXOutputs中index为voutindex的TXOutput被后面的某个区块引用. 也就是向前遍历区块交易时，所有区块的tx.Vin所引用的交易交易输出都会被加入到这个map.
	bci := bc.Iterator()

	for {
		block := bci.Next() //遍历block, 遍历的方式是从最新的Block开始
		//最新的block的TXOutput一定是UTXO.
		//block为height的区块中的tx,只被被下一个区块引用, 如果从最后一个区块反向遍历到height区块, 就能找到height区块中教的的TXOutput,所有被引用的TXOutput,剩下的就是UTXO. 所以从最新区块向上遍历，tx.TXInput能不断被发现被消耗的UTXO.
		//block内的区块的transaction之间不会发生引用. 因为还没有落账.
		//所以当后面的区块遍历完之后，就知道了当前区块哪些tx被引用的。而没有被引用的一定是UTXO, 这个被引用的集合为spentOutIdx.

		for _, tx := range block.Transactions { //遍历block中的tx
			txID := hex.EncodeToString(tx.ID)

		Outputs:
			/*for循环遍历tx.Vout.检查当前交易中未被使用的TXOutput. 当前区块的之间的交易不会引用,而当前区块已经被花费的交易输出已经记录在了spentTXOs中，所以直接比较spentTXOs，没有记录的就可以加入UTXO了。*/
			for outIdx, out := range tx.Vout { //遍历tx中的TXOutput. 是一个O3的3层循环.
				// Was the output spent?
				if spentTXOs[txID] != nil { //如果不为空说明该交易的Vout被引用
					for _, spentOutIdx := range spentTXOs[txID] {
						if spentOutIdx == outIdx { //检查被引用的Vout是否和当前的outidx是否相等.
							continue Outputs //如果当前检查的outidx在spentTXOs中.则跳过，不加入UTXO。继续建一个voutindex检查
						}
					}
				}

				outs := UTXO[txID]
				outs.Outputs = append(outs.Outputs, out) //最新block的第一次的out都直接加入UTXO set
				UTXO[txID] = outs
			}

			if tx.IsCoinbase() == false {
				for _, in := range tx.Vin { //遍历tx所有的使用, 所有Vin的引用必然是使用了.
					inTxID := hex.EncodeToString(in.Txid)
					spentTXOs[inTxID] = append(spentTXOs[inTxID], in.Vout)
					//这里的inTxID应该不在当前区块中. 而且该区块是更老的区块.
				}
			}
		}

		if len(block.PrevBlockHash) == 0 { //到了创始区块,没有其他的区块了,break
			break
		}
	}

	return UTXO
}

// Iterator returns a BlockchainIterat
func (bc *Blockchain) Iterator() *BlockchainIterator {
	bci := &BlockchainIterator{bc.tip, bc.db}

	return bci
}

// GetBestHeight returns the height of the latest block.
//返回最新块的高度.
func (bc *Blockchain) GetBestHeight() int {
	var lastBlock Block

	err := bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		lastHash := b.Get([]byte("l"))
		blockData := b.Get(lastHash)
		lastBlock = *DeserializeBlock(blockData)

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	return lastBlock.Height
}

// GetBlock finds a block by its hash and returns it
//根据block hash获取block。 比较直接， 因为block本身就是 blockhash(key) ==> blockdata(block序列化数据)的映射.
func (bc *Blockchain) GetBlock(blockHash []byte) (Block, error) {
	var block Block

	err := bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))

		blockData := b.Get(blockHash)

		if blockData == nil {
			return errors.New("Block is not found.")
		}

		block = *DeserializeBlock(blockData)

		return nil
	})
	if err != nil {
		return block, err
	}

	return block, nil
}

// GetBlockHashes returns a list of hashes of all the blocks in the chain
//获取所有block的hash数组.
func (bc *Blockchain) GetBlockHashes() [][]byte {
	var blocks [][]byte //数组， 数组的元素是[]byte
	bci := bc.Iterator()

	for {
		block := bci.Next()

		blocks = append(blocks, block.Hash)

		if len(block.PrevBlockHash) == 0 {
			break
		}
	}

	return blocks
}

// MineBlock mines a new block with the provided transactions
//挖矿.将收到的交易打包成block. 新建block的同时本身就会挖矿. 挖矿，即是将交易打包成block。所以参数应该是交易序列。
func (bc *Blockchain) MineBlock(transactions []*Transaction) *Block {
	var lastHash []byte
	var lastHeight int

	for _, tx := range transactions {
		// TODO: ignore transaction if it's not valid
		if bc.VerifyTransaction(tx) != true { //交易验签.
			log.Panic("ERROR: Invalid transaction")
		}
	}

	err := bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		lastHash = b.Get([]byte("l")) //blocksBucket中，"l"这个特殊的Key存放lastBlock的hash值. 因为是链，所以只需要找到最后一个block，则所有的block都可以找到.

		blockData := b.Get(lastHash)
		block := DeserializeBlock(blockData)

		lastHeight = block.Height //获取当前区块高度.

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	newBlock := NewBlock(transactions, lastHash, lastHeight+1) //生成一个新块，这个过程会挖矿.
	//如果收到先算出的块怎么办.

	err = bc.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		err := b.Put(newBlock.Hash, newBlock.Serialize()) //已block的hash为key，存储block的序列化数据.
		if err != nil {
			log.Panic(err)
		}

		err = b.Put([]byte("l"), newBlock.Hash) //设置last hash值
		if err != nil {
			log.Panic(err)
		}

		bc.tip = newBlock.Hash //Blockchain.tip始终存储最新块的hash值.

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	return newBlock
}

// SignTransaction signs inputs of a Transaction
//传入的Transaction是查tx.TXInput.Signature数据的transaction
/*
1. 存储在已解锁输出的公钥哈希。它识别了一笔交易的“发送方”。
2. 存储在新的锁定输出里面的公钥哈希。它识别了一笔交易的“接收方”。
3. 新的输出值。
这几个数据被签名后，意味着任何手段都不能单独修改。因为核心数据被签名了.
*/
func (bc *Blockchain) SignTransaction(tx *Transaction, privKey ecdsa.PrivateKey) {
	prevTXs := make(map[string]Transaction) //string是通过tx.ID转换而来的. prevTXs用于存放需要签名的input依赖的交易.

	for _, vin := range tx.Vin {
		prevTX, err := bc.FindTransaction(vin.Txid)
		if err != nil {
			log.Panic(err)
		}
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}

	tx.Sign(privKey, prevTXs) // tx是待签名的交易. prevTXs是引用的交易.
}

// VerifyTransaction verifies transaction input signatures
func (bc *Blockchain) VerifyTransaction(tx *Transaction) bool {
	if tx.IsCoinbase() { //Coinbase不需要验签.
		return true
	}

	prevTXs := make(map[string]Transaction)

	for _, vin := range tx.Vin {
		prevTX, err := bc.FindTransaction(vin.Txid)
		if err != nil {
			log.Panic(err)
		}
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX //和签名一样，先找到当前tx依赖的所有transaction
	}

	return tx.Verify(prevTXs)
}

func dbExists(dbFile string) bool {
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		return false
	}

	return true
}
