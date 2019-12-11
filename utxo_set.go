package main

import (
	"encoding/hex"
	"log"

	"github.com/boltdb/bolt"
)

/*k(txid), v(TXOutputs)注意这里的TXOutputs仅仅只是txid中未被花费的输出.*/
const utxoBucket = "chainstate" //所有用户未花费输出的Bucket.

// UTXOSet represents UTXO set
type UTXOSet struct {
	Blockchain *Blockchain
}

// FindSpendableOutputs finds and returns unspent outputs to reference in inputs
//查找区块链上pubkeyHash账户utxo集合.直到这些集合累计未花费金额达到amount为止.
/*比特币P2KH的解锁脚本+锁定脚本验证两个方面。 解锁脚本是交易发起人的签名数据和公钥。用于验证交易发起人。 而锁定脚本包含所有权拥有者公钥*/
func (u UTXOSet) FindSpendableOutputs(pubkeyHash []byte, amount int) (int, map[string][]int) {
	unspentOutputs := make(map[string][]int) //TXID -> outIDx的映射.
	accumulated := 0
	db := u.Blockchain.db

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket))
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() { //key表示交易ID. value是这个交易中未被引用的所有output集合.
			txID := hex.EncodeToString(k)
			outs := DeserializeOutputs(v) //返回的是TXOutput数组.

			for outIdx, out := range outs.Outputs {
				if out.IsLockedWithKey(pubkeyHash) && accumulated < amount { //检查所有权。
					accumulated += out.Value
					unspentOutputs[txID] = append(unspentOutputs[txID], outIdx)
				}
			}
		}

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	return accumulated, unspentOutputs
}

// FindUTXO finds UTXO for a public key hash
/*UTXO是账本，以key(txid), v(TXOutput)(注意这里的TXOutput)存储, 所以遍历的是交易id*/
func (u UTXOSet) FindUTXO(pubKeyHash []byte) []TXOutput {
	var UTXOs []TXOutput
	db := u.Blockchain.db

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket))
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() { //所以,数据库中存放的是TXID->TXOutput的映射
			outs := DeserializeOutputs(v)

			for _, out := range outs.Outputs {
				if out.IsLockedWithKey(pubKeyHash) { //检查PubKeyHash是否对资产拥有所有权.
					UTXOs = append(UTXOs, out)
				}
			}
		}

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	return UTXOs
}

// CountTransactions returns the number of transactions in the UTXO set
//计算UTXO set中transaction的数量.
func (u UTXOSet) CountTransactions() int {
	db := u.Blockchain.db
	counter := 0

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket))
		c := b.Cursor()

		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			counter++
		}

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	return counter
}

// Reindex rebuilds the UTXO set. 重构UTXOSet
func (u UTXOSet) Reindex() {
	db := u.Blockchain.db
	bucketName := []byte(utxoBucket)

	err := db.Update(func(tx *bolt.Tx) error {
		err := tx.DeleteBucket(bucketName)
		if err != nil && err != bolt.ErrBucketNotFound {
			log.Panic(err)
		}

		_, err = tx.CreateBucket(bucketName) //删除旧的utxoBucket,并创建新的utxoBucket.
		if err != nil {
			log.Panic(err)
		}

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	//创建成功.
	UTXO := u.Blockchain.FindUTXO() //从Blockchain获取UTXO set

	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)

		for txID, outs := range UTXO { //将UTXO写入到数据库. 以后会更新区块后同步写入吗？在哪里? 再UTXOSet.Update中.
			key, err := hex.DecodeString(txID)
			if err != nil {
				log.Panic(err)
			}

			err = b.Put(key, outs.Serialize())
			if err != nil {
				log.Panic(err)
			}
		}

		return nil
	})
}

// Update updates the UTXO set with transactions from the Block
// The Block is considered to be the tip of a blockchain
func (u UTXOSet) Update(block *Block) {
	db := u.Blockchain.db

	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket)) //获取utxoBucket

		for _, tx := range block.Transactions {
			if tx.IsCoinbase() == false { //如果是普通交易，说明是要花费utxo的。这就要把相关花费的utxo删除.
				for _, vin := range tx.Vin {
					updatedOuts := TXOutputs{}
					outsBytes := b.Get(vin.Txid)          //获取引用交易所有的utxo
					outs := DeserializeOutputs(outsBytes) //获取引用交易所有的utxo

					for outIdx, out := range outs.Outputs {
						if outIdx != vin.Vout { //只要被vin引用，则不加入到对应txid更新后的Utxo中去。
							updatedOuts.Outputs = append(updatedOuts.Outputs, out)
						}
					}

					if len(updatedOuts.Outputs) == 0 { //如果更新后的utxo为0，则直接删除对应的txid表项，说明再该txid中已经不存在utxo了
						err := b.Delete(vin.Txid)
						if err != nil {
							log.Panic(err)
						}
					} else {
						err := b.Put(vin.Txid, updatedOuts.Serialize()) //将更新后的Utxo再次些会到存储.
						if err != nil {
							log.Panic(err)
						}
					}

				}
			}

			//如果是coinbase交易， 由于没有引用输入，就没有花费utxo，所以可直接将coinbase的TXOutput按照txid->TXOutput写入UTXO set
			newOutputs := TXOutputs{}
			for _, out := range tx.Vout {
				newOutputs.Outputs = append(newOutputs.Outputs, out)
			}

			err := b.Put(tx.ID, newOutputs.Serialize())
			if err != nil {
				log.Panic(err)
			}
		}

		return nil
	})
	if err != nil {
		log.Panic(err)
	}
}
