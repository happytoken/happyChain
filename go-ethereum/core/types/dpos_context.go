package types

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/happytoken/go-ethereum/common"
	"github.com/happytoken/go-ethereum/crypto/sha3"
	"github.com/happytoken/go-ethereum/rlp"
	"github.com/happytoken/go-ethereum/trie"
)

type DposContext struct {
	epochTrie     *trie.Trie   //记录每个周期的验证人列表
	delegateTrie  *trie.Trie   //记录验证人以及对应投票人的列表
	voteTrie      *trie.Trie   //记录投票人对应验证人
	candidateTrie *trie.Trie   //记录候选人列表
	mintCntTrie   *trie.Trie   //记录验证人在周期内的出块数目

	db *trie.Database
}

var (
	epochPrefix     = []byte("epoch-")
	delegatePrefix  = []byte("delegate-")
	votePrefix      = []byte("vote-")
	candidatePrefix = []byte("candidate-")
	mintCntPrefix   = []byte("mintCnt-")
)

func NewEpochTrie(root common.Hash, db *trie.Database) (*trie.Trie, error) {
	return trie.NewTrieWithPrefix(root, epochPrefix, db)
}

func NewDelegateTrie(root common.Hash, db *trie.Database) (*trie.Trie, error) {
	return trie.NewTrieWithPrefix(root, delegatePrefix, db)
}

func NewVoteTrie(root common.Hash, db *trie.Database) (*trie.Trie, error) {
	return trie.NewTrieWithPrefix(root, votePrefix, db)
}

func NewCandidateTrie(root common.Hash, db *trie.Database) (*trie.Trie, error) {
	return trie.NewTrieWithPrefix(root, candidatePrefix, db)
}

func NewMintCntTrie(root common.Hash, db *trie.Database) (*trie.Trie, error) {
	return trie.NewTrieWithPrefix(root, mintCntPrefix, db)
}

func NewDposContext(db *trie.Database) (*DposContext, error) {
	epochTrie, err := NewEpochTrie(common.Hash{}, db)
	if err != nil {
		return nil, err
	}
	delegateTrie, err := NewDelegateTrie(common.Hash{}, db)
	if err != nil {
		return nil, err
	}
	voteTrie, err := NewVoteTrie(common.Hash{}, db)
	if err != nil {
		return nil, err
	}
	candidateTrie, err := NewCandidateTrie(common.Hash{}, db)
	if err != nil {
		return nil, err
	}
	mintCntTrie, err := NewMintCntTrie(common.Hash{}, db)
	if err != nil {
		return nil, err
	}
	return &DposContext{
		epochTrie:     epochTrie,
		delegateTrie:  delegateTrie,
		voteTrie:      voteTrie,
		candidateTrie: candidateTrie,
		mintCntTrie:   mintCntTrie,
		db:            db,
	}, nil
}

func NewDposContextFromProto(db *trie.Database, ctxProto *DposContextProto) (*DposContext, error) {
	epochTrie, err := NewEpochTrie(ctxProto.EpochHash, db)
	if err != nil {
		return nil, err
	}
	delegateTrie, err := NewDelegateTrie(ctxProto.DelegateHash, db)
	if err != nil {
		return nil, err
	}
	voteTrie, err := NewVoteTrie(ctxProto.VoteHash, db)
	if err != nil {
		return nil, err
	}
	candidateTrie, err := NewCandidateTrie(ctxProto.CandidateHash, db)
	if err != nil {
		return nil, err
	}
	mintCntTrie, err := NewMintCntTrie(ctxProto.MintCntHash, db)
	if err != nil {
		return nil, err
	}
	return &DposContext{
		epochTrie:     epochTrie,
		delegateTrie:  delegateTrie,
		voteTrie:      voteTrie,
		candidateTrie: candidateTrie,
		mintCntTrie:   mintCntTrie,
		db:            db,
	}, nil
}

func (d *DposContext) Copy() *DposContext {
	epochTrie := *d.epochTrie
	delegateTrie := *d.delegateTrie
	voteTrie := *d.voteTrie
	candidateTrie := *d.candidateTrie
	mintCntTrie := *d.mintCntTrie
	return &DposContext{
		epochTrie:     &epochTrie,
		delegateTrie:  &delegateTrie,
		voteTrie:      &voteTrie,
		candidateTrie: &candidateTrie,
		mintCntTrie:   &mintCntTrie,
	}
}

func (d *DposContext) Root() (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, d.epochTrie.Hash())
	rlp.Encode(hw, d.delegateTrie.Hash())
	rlp.Encode(hw, d.candidateTrie.Hash())
	rlp.Encode(hw, d.voteTrie.Hash())
	rlp.Encode(hw, d.mintCntTrie.Hash())
	hw.Sum(h[:0])
	return h
}

func (d *DposContext) Snapshot() *DposContext {
	return d.Copy()
}

func (d *DposContext) RevertToSnapShot(snapshot *DposContext) {
	d.epochTrie = snapshot.epochTrie
	d.delegateTrie = snapshot.delegateTrie
	d.candidateTrie = snapshot.candidateTrie
	d.voteTrie = snapshot.voteTrie
	d.mintCntTrie = snapshot.mintCntTrie
}

func (d *DposContext) FromProto(dcp *DposContextProto) error {
	var err error

	d.epochTrie, err = NewEpochTrie(dcp.EpochHash, d.db)
	if err != nil {
		return err
	}
	d.delegateTrie, err = NewDelegateTrie(dcp.DelegateHash, d.db)
	if err != nil {
		return err
	}
	d.candidateTrie, err = NewCandidateTrie(dcp.CandidateHash, d.db)
	if err != nil {
		return err
	}
	d.voteTrie, err = NewVoteTrie(dcp.VoteHash, d.db)
	if err != nil {
		return err
	}
	d.mintCntTrie, err = NewMintCntTrie(dcp.MintCntHash, d.db)
	return err
}

type DposContextProto struct {
	EpochHash     common.Hash `json:"epochRoot"        gencodec:"required"`
	DelegateHash  common.Hash `json:"delegateRoot"     gencodec:"required"`
	CandidateHash common.Hash `json:"candidateRoot"    gencodec:"required"`
	VoteHash      common.Hash `json:"voteRoot"         gencodec:"required"`
	MintCntHash   common.Hash `json:"mintCntRoot"      gencodec:"required"`
}

func (d *DposContext) ToProto() *DposContextProto {
	return &DposContextProto{
		EpochHash:     d.epochTrie.Hash(),
		DelegateHash:  d.delegateTrie.Hash(),
		CandidateHash: d.candidateTrie.Hash(),
		VoteHash:      d.voteTrie.Hash(),
		MintCntHash:   d.mintCntTrie.Hash(),
	}
}

func (p *DposContextProto) Root() (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, p.EpochHash)
	rlp.Encode(hw, p.DelegateHash)
	rlp.Encode(hw, p.CandidateHash)
	rlp.Encode(hw, p.VoteHash)
	rlp.Encode(hw, p.MintCntHash)
	hw.Sum(h[:0])
	return h
}

func (d *DposContext) KickoutCandidate(candidateAddr common.Address) error {
	candidate := candidateAddr.Bytes()
	err := d.candidateTrie.TryDelete(candidate)
	if err != nil {
		if _, ok := err.(*trie.MissingNodeError); !ok {
			return err
		}
	}
	iter := trie.NewIterator(d.delegateTrie.PrefixIterator(candidate))
	for iter.Next() {
		delegator := iter.Value
		key := append(candidate, delegator...)
		err = d.delegateTrie.TryDelete(key)
		if err != nil {
			if _, ok := err.(*trie.MissingNodeError); !ok {
				return err
			}
		}
		v, err := d.voteTrie.TryGet(delegator)
		if err != nil {
			if _, ok := err.(*trie.MissingNodeError); !ok {
				return err
			}
		}
		if err == nil && bytes.Equal(v, candidate) {
			err = d.voteTrie.TryDelete(delegator)
			if err != nil {
				if _, ok := err.(*trie.MissingNodeError); !ok {
					return err
				}
			}
		}
	}
	return nil
}

func (d *DposContext) BecomeCandidate(candidateAddr common.Address) error {
	// 当出块前检查内部交易类型，如果类型为1（RegCandidate）更新候选人树(数据库)
	candidate := candidateAddr.Bytes()
	return d.candidateTrie.TryUpdate(candidate, candidate)
}

//用户投票
func (d *DposContext) Delegate(delegatorAddr, candidateAddr common.Address) error {
	delegator, candidate := delegatorAddr.Bytes(), candidateAddr.Bytes()

	// the candidate must be candidate
	// 投票(授权)之前需要先检查该账号是否候选人
	candidateInTrie, err := d.candidateTrie.TryGet(candidate)
	if err != nil {
		return err
	}
	if candidateInTrie == nil {
		return errors.New("invalid candidate to delegate")
	}

	// delete old candidate if exists
	// 如果投票人之前已经给其他人投过票则先取消之前的投票
	oldCandidate, err := d.voteTrie.TryGet(delegator)
	if err != nil {
		if _, ok := err.(*trie.MissingNodeError); !ok {
			return err
		}
	}
	if oldCandidate != nil {
		d.delegateTrie.Delete(append(oldCandidate, delegator...))
	}
	// 更新候选人对应的授权列表
	if err = d.delegateTrie.TryUpdate(append(candidate, delegator...), delegator); err != nil {
		return err
	}
	//更新投票人对应的候选人列表
	return d.voteTrie.TryUpdate(delegator, candidate)
}

//取消投票--删除投票人对应的候选人列表及候选人对应的投票人列表信息
func (d *DposContext) UnDelegate(delegatorAddr, candidateAddr common.Address) error {
	//地址解析为bytes类型
	delegator, candidate := delegatorAddr.Bytes(), candidateAddr.Bytes()

	// 检查所取消投票的候选人中是否是在候选人列表中
	candidateInTrie, err := d.candidateTrie.TryGet(candidate)
	if err != nil {
		return err
	}

	if candidateInTrie == nil {
		return errors.New("invalid candidate to undelegate")
	}

	// 检查投票人自身是的投票列表中是否有投票记录
	oldCandidate, err := d.voteTrie.TryGet(delegator)
	if err != nil {
		return err
	}

	//检查所取消投票的候选人是否在VoteTrie（投票人对应投票候选人列表中）
	if !bytes.Equal(candidate, oldCandidate) {
		return errors.New("mismatch candidate to undelegate")
	}

	// 删除候选人对应投票人的列表中
	if err = d.delegateTrie.TryDelete(append(candidate, delegator...)); err != nil {
		return err
	}
	//删除投票人自身列表中的候选人列表
	return d.voteTrie.TryDelete(delegator)
}


func (d *DposContext) Commit() (*DposContextProto, error) {

	epochRoot, err := d.epochTrie.Commit(nil)
	if err != nil {
		return nil, err
	}
	d.epochTrie.TryUpdate(epochRoot[:], d.epochTrie.Get(epochRoot[:]))


	delegateRoot, err := d.delegateTrie.Commit(nil)
	if err != nil {
		return nil, err
	}
	d.delegateTrie.TryUpdate(delegateRoot[:], d.delegateTrie.Get(delegateRoot[:]))

	voteRoot, err := d.voteTrie.Commit(nil)
	if err != nil {
		return nil, err
	}
	d.voteTrie.TryUpdate(voteRoot[:], d.voteTrie.Get(voteRoot[:]))

	candidateRoot, err := d.candidateTrie.Commit(nil)
	if err != nil {
		return nil, err
	}
	d.candidateTrie.TryUpdate(candidateRoot[:], d.candidateTrie.Get(candidateRoot[:]))

	mintCntRoot, err := d.mintCntTrie.Commit(nil)
	if err != nil {
		return nil, err
	}
	d.mintCntTrie.TryUpdate(mintCntRoot[:], d.mintCntTrie.Get(mintCntRoot[:]))

	d.db.Commit(epochRoot,true)
	d.db.Commit(delegateRoot,true)
	d.db.Commit(candidateRoot,true)
	d.db.Commit(voteRoot,true)
	d.db.Commit(mintCntRoot,true)

	return &DposContextProto{
		EpochHash:     epochRoot,
		DelegateHash:  delegateRoot,
		VoteHash:      voteRoot,
		CandidateHash: candidateRoot,
		MintCntHash:   mintCntRoot,
	}, nil
}

func (d *DposContext) CandidateTrie() *trie.Trie          { return d.candidateTrie }
func (d *DposContext) DelegateTrie() *trie.Trie           { return d.delegateTrie }
func (d *DposContext) VoteTrie() *trie.Trie               { return d.voteTrie }
func (d *DposContext) EpochTrie() *trie.Trie              { return d.epochTrie }
func (d *DposContext) MintCntTrie() *trie.Trie            { return d.mintCntTrie }
func (d *DposContext) DB() *trie.Database                 { return d.db }
func (dc *DposContext) SetEpoch(epoch *trie.Trie)         { dc.epochTrie = epoch }
func (dc *DposContext) SetDelegate(delegate *trie.Trie)   { dc.delegateTrie = delegate }
func (dc *DposContext) SetVote(vote *trie.Trie)           { dc.voteTrie = vote }
func (dc *DposContext) SetCandidate(candidate *trie.Trie) { dc.candidateTrie = candidate }
func (dc *DposContext) SetMintCnt(mintCnt *trie.Trie)     { dc.mintCntTrie = mintCnt }

func (dc *DposContext) GetValidators() ([]common.Address, error) {
	var validators []common.Address
	key := []byte("validator")
	validatorsRLP := dc.epochTrie.Get(key)
	if err := rlp.DecodeBytes(validatorsRLP, &validators); err != nil {
		return nil, fmt.Errorf("failed to decode validators: %s", err)
	}
	return validators, nil
}

func (dc *DposContext) SetValidators(validators []common.Address) error {
	key := []byte("validator")
	validatorsRLP, err := rlp.EncodeToBytes(validators)
	if err != nil {
		return fmt.Errorf("failed to encode validators to rlp bytes: %s", err)
	}
	dc.epochTrie.Update(key, validatorsRLP)
	return nil
}
