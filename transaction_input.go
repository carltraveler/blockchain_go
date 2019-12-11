package main

import "bytes"

// TXInput represents a transaction input
type TXInput struct {
	Txid      []byte //该输入使用的是哪个交易.对于Coinbase，该值为0
	Vout      int    //使用的Txid中的第几个输出.也就是TXOutput.Vout[Vout], 对于Coinbase,该值为-1
	Signature []byte //Signature和PubKey都是解锁脚本数据. 在一个交易中用支付人的私钥加密该member为nil的transaction即该数据.
	PubKey    []byte //公钥.用于在解锁的时候，比较引用的TXOutput中的公钥. 用于验证. 在一个交易中，用支付人的公钥填写.
}

// UsesKey checks whether the address initiated the transaction
func (in *TXInput) UsesKey(pubKeyHash []byte) bool {
	lockingHash := HashPubKey(in.PubKey)

	return bytes.Compare(lockingHash, pubKeyHash) == 0
}
