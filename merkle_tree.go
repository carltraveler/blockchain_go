package main

import (
	"crypto/sha256"
)

// MerkleTree represent a Merkle tree
type MerkleTree struct {
	RootNode *MerkleNode
}

// MerkleNode represent a Merkle tree node
type MerkleNode struct {
	Left  *MerkleNode
	Right *MerkleNode
	Data  []byte
}

// NewMerkleTree creates a new Merkle tree from a sequence of data
func NewMerkleTree(data [][]byte) *MerkleTree { //[][]byte存放的是Transaction序列化后的数据.一个元素代表一个Transaction.
	var nodes []MerkleNode

	if len(data)%2 != 0 { //如果Transaction个数是奇数.
		data = append(data, data[len(data)-1])
	}

	for _, datum := range data { //遍历Transaction
		node := NewMerkleNode(nil, nil, datum)
		nodes = append(nodes, *node) //没有形成树, 只有树的最底部叶子节点.
	}

	for i := 0; i < len(data)/2; i++ { //底部节点现在是偶数了，所以最多遍历len(data)/2次.
		var newLevel []MerkleNode

		for j := 0; j < len(nodes); j += 2 { //j += 2代表偶数前进，每次处理2个, 以Nodes为基底，生成上一层节点.
			node := NewMerkleNode(&nodes[j], &nodes[j+1], nil)
			newLevel = append(newLevel, *node)
		}

		nodes = newLevel //newLevel为新的基地, 算法不应该是newLevel只有一个节点?
	}

	mTree := MerkleTree{&nodes[0]}

	return &mTree
}

// NewMerkleNode creates a new Merkle tree node, data是node的数据.
// 如果left, right不为nil，则表示使用left, right向上生成一个非叶子节点.
func NewMerkleNode(left, right *MerkleNode, data []byte) *MerkleNode {
	mNode := MerkleNode{}

	if left == nil && right == nil {
		hash := sha256.Sum256(data)
		mNode.Data = hash[:]
	} else {
		prevHashes := append(left.Data, right.Data...)
		hash := sha256.Sum256(prevHashes)
		mNode.Data = hash[:]
	}

	mNode.Left = left
	mNode.Right = right

	return &mNode
}
