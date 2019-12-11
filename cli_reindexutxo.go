package main

import "fmt"

func (cli *CLI) reindexUTXO(nodeID string) {
	bc := NewBlockchain(nodeID) //根据现有的存储构建区块链.
	UTXOSet := UTXOSet{bc}
	UTXOSet.Reindex() //重构UTXOSet

	count := UTXOSet.CountTransactions()
	fmt.Printf("Done! There are %d transactions in the UTXO set.\n", count)
}
