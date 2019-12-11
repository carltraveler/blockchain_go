package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"math/big"
	"strings"

	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
)

const subsidy = 10 //挖矿奖励的金额，包括创始区块.

// Transaction represents a Bitcoin transaction
type Transaction struct { //由于Vin是引用的TXOutput,所以在block中，真正存放的是TXID->[]TXOutput数组的映射.
	ID   []byte     // tx.hash()计算获取.不带签名数据.
	Vin  []TXInput  //交易输入序列
	Vout []TXOutput //交易输出
}

// IsCoinbase checks whether the transaction is coinbase,检查交易是否为coinbase交易.
func (tx Transaction) IsCoinbase() bool {
	return len(tx.Vin) == 1 && len(tx.Vin[0].Txid) == 0 && tx.Vin[0].Vout == -1
}

// Serialize returns a serialized Transaction
func (tx Transaction) Serialize() []byte {
	var encoded bytes.Buffer

	enc := gob.NewEncoder(&encoded)
	err := enc.Encode(tx)
	if err != nil {
		log.Panic(err)
	}

	return encoded.Bytes()
}

// Hash returns the hash of the Transaction
func (tx *Transaction) Hash() []byte {
	var hash [32]byte

	txCopy := *tx
	txCopy.ID = []byte{} //why?

	hash = sha256.Sum256(txCopy.Serialize())

	return hash[:]
}

// Sign signs each input of a Transaction
func (tx *Transaction) Sign(privKey ecdsa.PrivateKey, prevTXs map[string]Transaction) {
	if tx.IsCoinbase() { //coinbase不用申明UTXO所有权，直接打给受益人.也就不需要签名
		return
	}

	for _, vin := range tx.Vin {
		if prevTXs[hex.EncodeToString(vin.Txid)].ID == nil {
			log.Panic("ERROR: Previous transaction is not correct")
		}
	}

	txCopy := tx.TrimmedCopy() //为什么trimmed coppy没有input的公钥数据. 去除tx.TXInput.Signature和tx.TXInput.PubKey.设置未nil. 注意tx是当前交易

	for inID, vin := range txCopy.Vin {
		prevTx := prevTXs[hex.EncodeToString(vin.Txid)]
		txCopy.Vin[inID].Signature = nil
		txCopy.Vin[inID].PubKey = prevTx.Vout[vin.Vout].PubKeyHash //这里和直接填写签名者的公钥有什么区别.
		//找的prevTX的原因就是找到PubKeyHash? 不应该和签名者一样吗. 这句话应该可以注释掉吧

		dataToSign := fmt.Sprintf("%x\n", txCopy)

		r, s, err := ecdsa.Sign(rand.Reader, &privKey, []byte(dataToSign))
		if err != nil {
			log.Panic(err)
		}
		signature := append(r.Bytes(), s.Bytes()...)

		tx.Vin[inID].Signature = signature
		txCopy.Vin[inID].PubKey = nil
	}
}

// String returns a human-readable representation of a transaction
func (tx Transaction) String() string {
	var lines []string

	lines = append(lines, fmt.Sprintf("--- Transaction %x:", tx.ID))

	for i, input := range tx.Vin {

		lines = append(lines, fmt.Sprintf("     Input %d:", i))
		lines = append(lines, fmt.Sprintf("       TXID:      %x", input.Txid))
		lines = append(lines, fmt.Sprintf("       Out:       %d", input.Vout))
		lines = append(lines, fmt.Sprintf("       Signature: %x", input.Signature))
		lines = append(lines, fmt.Sprintf("       PubKey:    %x", input.PubKey))
	}

	for i, output := range tx.Vout {
		lines = append(lines, fmt.Sprintf("     Output %d:", i))
		lines = append(lines, fmt.Sprintf("       Value:  %d", output.Value))
		lines = append(lines, fmt.Sprintf("       Script: %x", output.PubKeyHash))
	}

	return strings.Join(lines, "\n")
}

// TrimmedCopy creates a trimmed copy of Transaction to be used in signing
//将tx.Vin的Signature和PublicKey设置为nil.
func (tx *Transaction) TrimmedCopy() Transaction {
	var inputs []TXInput
	var outputs []TXOutput

	for _, vin := range tx.Vin {
		inputs = append(inputs, TXInput{vin.Txid, vin.Vout, nil, nil})
	}

	for _, vout := range tx.Vout {
		outputs = append(outputs, TXOutput{vout.Value, vout.PubKeyHash})
	}

	txCopy := Transaction{tx.ID, inputs, outputs} //该Transaction的inputs没有签名数据.

	return txCopy
}

// Verify verifies signatures of Transaction inputs
/*验签需要验证两个方面的信息，
首先， 确认交易的发起人公钥地址是拥有私钥者发起的。
然后，交易的发起人引用自己拥有所有权的未花费交易输出。*/
func (tx *Transaction) Verify(prevTXs map[string]Transaction) bool {
	if tx.IsCoinbase() {
		return true
	}

	for _, vin := range tx.Vin {
		if prevTXs[hex.EncodeToString(vin.Txid)].ID == nil {
			log.Panic("ERROR: Previous transaction is not correct")
		}
	}

	txCopy := tx.TrimmedCopy() //获取inputs中不带所有者签名数据的交易
	curve := elliptic.P256()

	for inID, vin := range tx.Vin {
		/*假设交易的发起人，随意引用不属于自己的unspendUTXO， 在这里验签的时候，是无法通过的*/
		prevTx := prevTXs[hex.EncodeToString(vin.Txid)]
		txCopy.Vin[inID].Signature = nil
		/*注意这里非常关键，由于Signature是交易发起人签名的，prevTX是自己引用的，而用自己引用的交易的vout.PubKeyHash填写txCopy.Vin.Pubkey用来验签，假设自己引用的prevTx.Vout.PubKeyHash不是自己，也就是这个未花费的交易，自己并没有所有权，而是引用别人的，那么txCopy.Signature和txCopy.Pubkey对就不能通过验签.*/
		txCopy.Vin[inID].PubKey = prevTx.Vout[vin.Vout].PubKeyHash // 待验签的数据.

		r := big.Int{}
		s := big.Int{}
		sigLen := len(vin.Signature)
		r.SetBytes(vin.Signature[:(sigLen / 2)])
		s.SetBytes(vin.Signature[(sigLen / 2):])

		x := big.Int{}
		y := big.Int{}
		keyLen := len(vin.PubKey)
		x.SetBytes(vin.PubKey[:(keyLen / 2)])
		y.SetBytes(vin.PubKey[(keyLen / 2):])

		dataToVerify := fmt.Sprintf("%x\n", txCopy)

		rawPubKey := ecdsa.PublicKey{Curve: curve, X: &x, Y: &y}
		if ecdsa.Verify(&rawPubKey, []byte(dataToVerify), &r, &s) == false {
			return false
		}
		txCopy.Vin[inID].PubKey = nil
	}

	return true
}

// NewCoinbaseTX creates a new coinbase transaction
//挖矿交易。不会引用输入. to是矿工地址.
func NewCoinbaseTX(to, data string) *Transaction {
	if data == "" { //data是交易附带信息.
		randData := make([]byte, 20)
		_, err := rand.Read(randData)
		if err != nil {
			log.Panic(err)
		}

		data = fmt.Sprintf("%x", randData)
	}

	txin := TXInput{[]byte{}, -1, nil, []byte(data)} //所以挖矿交易只有交易附带信息有意义.
	txout := NewTXOutput(subsidy, to)
	tx := Transaction{nil, []TXInput{txin}, []TXOutput{*txout}}
	tx.ID = tx.Hash()

	return &tx
}

// NewUTXOTransaction creates a new transaction
func NewUTXOTransaction(wallet *Wallet, to string, amount int, UTXOSet *UTXOSet) *Transaction { //UTXOSet代表所有用户未花费的uxto集合. Wallet是支付人的钱包数据. to是收款人的地址. amount是收款的金额.
	var inputs []TXInput
	var outputs []TXOutput

	pubKeyHash := HashPubKey(wallet.PublicKey)
	acc, validOutputs := UTXOSet.FindSpendableOutputs(pubKeyHash, amount)

	if acc < amount { //检查是否获取到大于amount金额的utxo. 这些UTXO,PubKeyHash拥有所有权,所以是会填写到transaction的input中去的.
		log.Panic("ERROR: Not enough funds")
	}

	// Build a list of inputs
	for txid, outs := range validOutputs { //validOutputs是Txid -> TXOutputid的映射
		txID, err := hex.DecodeString(txid)
		if err != nil {
			log.Panic(err)
		}

		for _, out := range outs {
			input := TXInput{txID, out, nil, wallet.PublicKey}
			inputs = append(inputs, input) //构造inputs. 这些inputs中的金额大于等于所需的amount
		} //构造input
	}

	// Build a list of outputs
	from := fmt.Sprintf("%s", wallet.GetAddress()) // address通过公钥计算得来. HASH并校验
	outputs = append(outputs, *NewTXOutput(amount, to))
	if acc > amount {
		outputs = append(outputs, *NewTXOutput(acc-amount, from)) // a change. 将多余的金额返回给支付人.
	}

	tx := Transaction{nil, inputs, outputs}
	tx.ID = tx.Hash()                                          //tx.ID通过不带签名数据的transaction hash计算得到。也就是tx.TXInput.Signature为nil。其他值都正确设置.
	UTXOSet.Blockchain.SignTransaction(&tx, wallet.PrivateKey) //支付人签名.

	return &tx
}

// DeserializeTransaction deserializes a transaction
func DeserializeTransaction(data []byte) Transaction {
	var transaction Transaction

	decoder := gob.NewDecoder(bytes.NewReader(data))
	err := decoder.Decode(&transaction)
	if err != nil {
		log.Panic(err)
	}

	return transaction
}
