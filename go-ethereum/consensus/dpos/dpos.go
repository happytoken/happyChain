package dpos

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/happytoken/go-ethereum/accounts"
	"github.com/happytoken/go-ethereum/common"
	"github.com/happytoken/go-ethereum/consensus"
	"github.com/happytoken/go-ethereum/consensus/misc"
	"github.com/happytoken/go-ethereum/core/state"
	"github.com/happytoken/go-ethereum/core/types"
	"github.com/happytoken/go-ethereum/crypto"
	"github.com/happytoken/go-ethereum/crypto/sha3"
	"github.com/happytoken/go-ethereum/ethdb"
	"github.com/happytoken/go-ethereum/log"
	"github.com/happytoken/go-ethereum/params"
	"github.com/happytoken/go-ethereum/rlp"
	"github.com/happytoken/go-ethereum/rpc"
	"github.com/happytoken/go-ethereum/trie"
	lru "github.com/hashicorp/golang-lru"
)


const (
	extraVanity        = 32   // Fixed number of extra-data prefix bytes reserved for signer vanity
	extraSeal          = 65   // Fixed number of extra-data suffix bytes reserved for signer seal
	inmemorySignatures = 4096 // Number of recent block signatures to keep in memory

	//blockInterval    = int64(10)  	//出块间隔
	epochInterval    = int64(86400)  //选举周期间隔24 *60*60 s
	//maxValidatorSize = 21
	//safeSize         =  15	//maxValidatorSize*2/3 + 1
	//consensusSize    =  15 	//maxValidatorSize*2/3 + 1
)



var (
	big0  = big.NewInt(0)
	big8  = big.NewInt(8)
	big32 = big.NewInt(32)

	frontierBlockReward  *big.Int = big.NewInt(5e+18) // Block reward in wei for successfully mining a block
	byzantiumBlockReward *big.Int = big.NewInt(3e+18) // Block reward in wei for successfully mining a block upward from Byzantium

	timeOfFirstBlock = int64(0)

	confirmedBlockHead = []byte("confirmed-block-head")
)

var (
	// errUnknownBlock is returned when the list of signers is requested for a block
	// that is not part of the local blockchain.
	errUnknownBlock = errors.New("unknown block")
	// errMissingVanity is returned if a block's extra-data section is shorter than
	// 32 bytes, which is required to store the signer vanity.
	errMissingVanity = errors.New("extra-data 32 byte vanity prefix missing")
	// errMissingSignature is returned if a block's extra-data section doesn't seem
	// to contain a 65 byte secp256k1 signature.
	errMissingSignature = errors.New("extra-data 65 byte suffix signature missing")
	// errInvalidMixDigest is returned if a block's mix digest is non-zero.
	errInvalidMixDigest = errors.New("non-zero mix digest")
	// errInvalidUncleHash is returned if a block contains an non-empty uncle list.
	errInvalidUncleHash  = errors.New("non empty uncle hash")
	errInvalidDifficulty = errors.New("invalid difficulty")

	// ErrInvalidTimestamp is returned if the timestamp of a block is lower than
	// the previous block's timestamp + the minimum block period.
	ErrInvalidTimestamp           = errors.New("invalid timestamp")
	ErrWaitForPrevBlock           = errors.New("wait for last block arrived")
	ErrMintFutureBlock            = errors.New("mint the future block")
	ErrMismatchSignerAndValidator = errors.New("mismatch block signer and validator")
	ErrInvalidBlockValidator      = errors.New("invalid block validator")
	ErrInvalidMintBlockTime       = errors.New("invalid time to mint the block")
	ErrNilBlockHeader             = errors.New("nil block header returned")
)
var (
	uncleHash = types.CalcUncleHash(nil) // Always Keccak256(RLP([])) as uncles are meaningless outside of PoW.
)

type Dpos struct {
	config *params.DposConfig // Consensus engine configuration parameters
	db      ethdb.Database     // Database to store and retrieve snapshot checkpoints

	signer               common.Address
	signFn               SignerFn
	signatures           *lru.ARCCache // Signatures of recent blocks to speed up mining
	confirmedBlockHeader *types.Header

	mu   sync.RWMutex
	stop chan bool
}

type SignerFn func(accounts.Account, []byte) ([]byte, error)

// NOTE: sigHash was copy from clique
// sigHash returns the hash which is used as input for the proof-of-authority
// signing. It is the hash of the entire header apart from the 65 byte signature
// contained at the end of the extra data.
//
// Note, the method requires the extra data to be at least 65 bytes, otherwise it
// panics. This is done to avoid accidentally using both forms (signature present
// or not), which could be abused to produce different hashes for the same header.
func sigHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewKeccak256()

	rlp.Encode(hasher, []interface{}{
		header.ParentHash,
		header.UncleHash,
		header.Validator,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra[:len(header.Extra)-65], // Yes, this will panic if extra is too short
		header.MixDigest,
		header.Nonce,
		header.DposContext.Root(),
		header.MaxValidatorSize,			//add MaxValidatorSize
	})
	hasher.Sum(hash[:0])

	return hash
}

func New(config *params.DposConfig, db ethdb.Database) *Dpos {
	signatures, _ := lru.NewARC(inmemorySignatures)

	return &Dpos{
		config:     config,
		db:         db,
		signatures: signatures,
	}
}

func (d *Dpos) Author(header *types.Header) (common.Address, error) {
	return header.Validator, nil
}

//Verify that the bulk complies with the consensus algorithm rules
func (d *Dpos) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool, blockInterval uint64) error {
	return d.verifyHeader(chain, header, nil, blockInterval)
}

func (d *Dpos) verifyHeader(chain consensus.ChainReader, header *types.Header, parents []*types.Header,blockInterval uint64 ) error {
	if header.Number == nil {
		return errUnknownBlock
	}
	number := header.Number.Uint64()
	// Unnecssary to verify the block from feature
	if header.Time.Cmp(big.NewInt(time.Now().Unix())) > 0 {
		return consensus.ErrFutureBlock
	}
	// Check that the extra-data contains both the vanity and signature
	if len(header.Extra) < extraVanity {
		return errMissingVanity
	}
	if len(header.Extra) < extraVanity+extraSeal {
		return errMissingSignature
	}
	// Ensure that the mix digest is zero as we don't have fork protection currently
	if header.MixDigest != (common.Hash{}) {
		return errInvalidMixDigest
	}
	// Difficulty always 1
	//难度设定为1	
	//header.Difficulty = big.NewInt(1)	
	if header.Difficulty.Uint64() != 1 {		
		return errInvalidDifficulty
	}

	// Ensure that the block doesn't contain any uncles which are meaningless in DPoS
	if header.UncleHash != uncleHash {
		return errInvalidUncleHash
	}
	// If all checks passed, validate any special fields for hard forks
	if err := misc.VerifyForkHashes(chain.Config(), header, false); err != nil {
		return err
	}

	var parent *types.Header
	if len(parents) > 0 {
		parent = parents[len(parents)-1]
	} else {
		parent = chain.GetHeader(header.ParentHash, number-1)
	}
	if parent == nil || parent.Number.Uint64() != number-1 || parent.Hash() != header.ParentHash {
		return consensus.ErrUnknownAncestor
	}
	if parent.Time.Uint64()+blockInterval> header.Time.Uint64() {
		return ErrInvalidTimestamp
	}
	return nil
}

//批量验证区块头是否符合共识算法规则
func (d *Dpos) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool,) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))
	blockInterval := chain.GetHeaderByNumber(0).BlockInterval

	go func() {
		for i, header := range headers {
			//header.Extra = make([]byte, extraVanity+extraSeal)
			err := d.verifyHeader(chain, header, headers[:i],blockInterval)
			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

// VerifyUncles implements consensus.Engine, always returning an error for any
// uncles as this consensus mechanism doesn't permit uncles.
func (d *Dpos) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errors.New("uncles not allowed")
	}
	return nil
}

// VerifySeal implements consensus.Engine, checking whether the signature contained
// in the header satisfies the consensus protocol requirements.
func (d *Dpos) VerifySeal(chain consensus.ChainReader, currentheader, genesisheader *types.Header) error {
	return d.verifySeal(chain, currentheader, genesisheader,nil)
}

func (d *Dpos) verifySeal(chain consensus.ChainReader, currentheader, genesisheader *types.Header, parents []*types.Header) error {
	// Verifying the genesis block is not supported
	number := currentheader.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}
	var parent *types.Header
	if len(parents) > 0 {
		parent = parents[len(parents)-1]
	} else {
		parent = chain.GetHeader(currentheader.ParentHash, number-1)
	}

	trieDB := trie.NewDatabase(d.db)
	dposContext, err := types.NewDposContextFromProto(trieDB, parent.DposContext) //todo nil

	if err != nil {
		return err
	}
	epochContext := &EpochContext{DposContext: dposContext}
	blockInterVal := genesisheader.BlockInterval
	validator, err := epochContext.lookupValidator(currentheader.Time.Int64(),blockInterVal)
	if err != nil {
		return err
	}
	//出块者签名验证
	if err := d.verifyBlockSigner(validator, currentheader); err != nil {
		return err
	}
	return d.updateConfirmedBlockHeader(chain)
}

func (d *Dpos) verifyBlockSigner(validator common.Address, header *types.Header) error {
	signer, err := ecrecover(header, d.signatures)
	if err != nil {
		return err
	}
	if bytes.Compare(signer.Bytes(), validator.Bytes()) != 0 {
		return ErrInvalidBlockValidator
	}
	if bytes.Compare(signer.Bytes(), header.Validator.Bytes()) != 0 {
		return ErrMismatchSignerAndValidator
	}
	return nil
}

func (d *Dpos) updateConfirmedBlockHeader(chain consensus.ChainReader) error {
	if d.confirmedBlockHeader == nil {
		header, err := d.loadConfirmedBlockHeader(chain)
		if err != nil {
			header = chain.GetHeaderByNumber(0)
			if header == nil {
				return err
			}
		}
		d.confirmedBlockHeader = header
	}

	curHeader := chain.CurrentHeader()

	fmt.Println("+++++++++++++++++++555555++++++++++++++++++++++\n")
	genesisHeader := chain.GetHeaderByNumber(0)
	fmt.Println("+++++++++++++++++++from genesisBlock to get Maxvalidatorsize++++++++++++++++++++++\n")
	epoch := int64(-1)
	validatorMap := make(map[common.Address]bool)
	for d.confirmedBlockHeader.Hash() != curHeader.Hash() &&
		d.confirmedBlockHeader.Number.Uint64() < curHeader.Number.Uint64() {
		curEpoch := curHeader.Time.Int64() / epochInterval
		if curEpoch != epoch {
			epoch = curEpoch
			validatorMap = make(map[common.Address]bool)
		}
		// fast return
		// if block number difference less consensusSize-witnessNum
		// there is no need to check block is confirmed
		consensusSize :=int(genesisHeader.MaxValidatorSize*2/3+1)
		if curHeader.Number.Int64()-d.confirmedBlockHeader.Number.Int64() < int64(consensusSize-len(validatorMap)) {
			log.Debug("Dpos fast return", "current", curHeader.Number.String(), "confirmed", d.confirmedBlockHeader.Number.String(), "witnessCount", len(validatorMap))
			return nil
		}
		validatorMap[curHeader.Validator] = true
		if len(validatorMap) >= consensusSize {
			d.confirmedBlockHeader = curHeader
			if err := d.storeConfirmedBlockHeader(d.db); err != nil {
				return err
			}
			log.Debug("dpos set confirmed block header success", "currentHeader", curHeader.Number.String())
			return nil
		}
		curHeader = chain.GetHeaderByHash(curHeader.ParentHash)
		if curHeader == nil {
			return ErrNilBlockHeader
		}
	}
	return nil
}

func (s *Dpos) loadConfirmedBlockHeader(chain consensus.ChainReader) (*types.Header, error) {
	key, err := s.db.Get(confirmedBlockHead)
	if err != nil {
		return nil, err
	}
	header := chain.GetHeaderByHash(common.BytesToHash(key))
	if header == nil {
		return nil, ErrNilBlockHeader
	}
	return header, nil
}

// store inserts the snapshot into the database.
func (s *Dpos) storeConfirmedBlockHeader(db ethdb.Database) error {
	return db.Put(confirmedBlockHead, s.confirmedBlockHeader.Hash().Bytes())
}

func (d *Dpos) Prepare(chain consensus.ChainReader, header *types.Header) error {
	header.Nonce = types.BlockNonce{}
	number := header.Number.Uint64()
	if len(header.Extra) < extraVanity {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, extraVanity-len(header.Extra))...)
	}
	header.Extra = header.Extra[:extraVanity]
	header.Extra = append(header.Extra, make([]byte, extraSeal)...)
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Difficulty = d.CalcDifficulty(chain, header.Time.Uint64(), parent)
	header.Validator = d.signer
	return nil
}

func AccumulateRewards(config *params.ChainConfig, state *state.StateDB, header *types.Header, uncles []*types.Header) {
	// Select the correct block reward based on chain progression
	blockReward := frontierBlockReward
	if config.IsByzantium(header.Number) {
		blockReward = byzantiumBlockReward
	}
	// Accumulate the rewards for the miner and any included uncles
	reward := new(big.Int).Set(blockReward)
	state.AddBalance(header.Coinbase, reward)
}

//将出块周期内的交易打包进新的区块中
func (d *Dpos) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt, dposContext *types.DposContext) (*types.Block, error) {
	// Accumulate block rewards and commit the final state root
	AccumulateRewards(chain.Config(), state, header, uncles)
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	parent := chain.GetHeaderByHash(header.ParentHash)
	epochContext := &EpochContext{
		statedb:     state,
		DposContext: dposContext,
		TimeStamp:   header.Time.Int64(),
	}
	if timeOfFirstBlock == 0 {
		if firstBlockHeader := chain.GetHeaderByNumber(1); firstBlockHeader != nil {
			timeOfFirstBlock = firstBlockHeader.Time.Int64()
		}
	}
	fmt.Println("++++++++++++++77777++++++++++++++++++\n")
	fmt.Println("**************get genesis header********\n")
	genesis := chain.GetHeaderByNumber(0)

	err := epochContext.tryElect(genesis, parent)
	if err != nil {
		return nil, fmt.Errorf("got error when elect next epoch, err: %s", err)
	}

	//update mint count trie
	updateMintCnt(parent.Time.Int64(), header.Time.Int64(), header.Validator, dposContext)
	header.DposContext = dposContext.ToProto()
	return types.NewBlock(header, txs, uncles, receipts), nil
}

func (d *Dpos) checkDeadline(lastBlock *types.Block, now int64, blockInterval uint64) error {
	prevSlot := PrevSlot(now, blockInterval)
	nextSlot := NextSlot(now, blockInterval)
	if lastBlock.Time().Int64() >= nextSlot {
		return ErrMintFutureBlock
	}
	// last block was arrived, or time's up
	if lastBlock.Time().Int64() == prevSlot || nextSlot-now <= 1 {
		return nil
	}
	return ErrWaitForPrevBlock
}

//检查当前的验证人是否在当前的节点上
func (d *Dpos) CheckValidator(lastBlock *types.Block, now int64,blockInterval uint64) error {
	if err := d.checkDeadline(lastBlock, now, blockInterval); err != nil {
		return err
	}
	//lastBlock.DposContext.DB()修改trie.NewDatabase(d.db)，解决没有创世块启动报错
	dposContext, err := types.NewDposContextFromProto(trie.NewDatabase(d.db), lastBlock.Header().DposContext)
	if err != nil {
		return err
	}
	epochContext := &EpochContext{DposContext: dposContext}
	validator, err := epochContext.lookupValidator(now,blockInterval)
	if err != nil {
		return err
	}
	if (validator == common.Address{}) || bytes.Compare(validator.Bytes(), d.signer.Bytes()) != 0 {
		return ErrInvalidBlockValidator
	}
	return nil
}

// Seal generates a new block for the given input block with the local miner's
// seal place on top.
//验证块内容是否符合dposS算法规则（验证新块是否是应该由该验证人来出块）
func (d *Dpos) Seal(chain consensus.ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error) {
	header := block.Header()
	number := header.Number.Uint64()
	// Sealing the genesis block is not supported
	if number == 0 {
		return nil, errUnknownBlock
	}
	now := time.Now().Unix()
	delay := NextSlot(now,chain.GetHeaderByNumber(0).BlockInterval) - now
	if delay > 0 {
		select {
		case <-stop:
			return nil, nil
		case <-time.After(time.Duration(delay) * time.Second):
		}
	}
	block.Header().Time.SetInt64(time.Now().Unix())

	// time's up, sign the block
	// 对新块进行签名
	sighash, err := d.signFn(accounts.Account{Address: d.signer}, sigHash(header).Bytes())
	if err != nil {
		return nil, err
	}
	copy(header.Extra[len(header.Extra)-extraSeal:], sighash)
	return block.WithSeal(header), nil
}

func (d *Dpos) CalcDifficulty(chain consensus.ChainReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (d *Dpos) APIs(chain consensus.ChainReader) []rpc.API {
	return []rpc.API{{
		Namespace: "dpos",
		Version:   "1.0",
		Service:   &API{chain: chain, dpos: d},
		Public:    true,
	}}
}

func (d *Dpos) Authorize(signer common.Address, signFn SignerFn) {
	d.mu.Lock()
	d.signer = signer
	d.signFn = signFn
	d.mu.Unlock()
}

func (d *Dpos) Close() error {
	return nil
}

// ecrecover extracts the Ethereum account address from a signed header.
func ecrecover(header *types.Header, sigcache *lru.ARCCache) (common.Address, error) {
	// If the signature's already cached, return that
	hash := header.Hash()
	if address, known := sigcache.Get(hash); known {
		return address.(common.Address), nil
	}
	// Retrieve the signature from the header extra-data
	if len(header.Extra) < extraSeal {
		return common.Address{}, errMissingSignature
	}
	signature := header.Extra[len(header.Extra)-extraSeal:]
	// Recover the public key and the Ethereum address
	pubkey, err := crypto.Ecrecover(sigHash(header).Bytes(), signature)
	if err != nil {
		return common.Address{}, err
	}
	var signer common.Address
	copy(signer[:], crypto.Keccak256(pubkey[1:])[12:])
	sigcache.Add(hash, signer)
	return signer, nil
}

func PrevSlot(now int64, blockInterval uint64) int64 {
	return int64((now-1)/int64(blockInterval)) * int64(blockInterval)
}

func NextSlot(now int64, blockInterval uint64) int64 {
	return int64((now+int64(blockInterval)-1)/int64(blockInterval)) * int64(blockInterval)
}

// update counts in MintCntTrie for the miner of newBlock
// 更新周期内验证人出块数目
func updateMintCnt(parentBlockTime, currentBlockTime int64, validator common.Address, dposContext *types.DposContext) {
	currentMintCntTrie := dposContext.MintCntTrie()
	currentEpoch := parentBlockTime / epochInterval
	currentEpochBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(currentEpochBytes, uint64(currentEpoch))

	cnt := int64(1)
	newEpoch := currentBlockTime / epochInterval
	// still during the currentEpochID
	if currentEpoch == newEpoch {
		iter := trie.NewIterator(currentMintCntTrie.NodeIterator(currentEpochBytes))

		// when current is not genesis, read last count from the MintCntTrie
		if iter.Next() {
			cntBytes := currentMintCntTrie.Get(append(currentEpochBytes, validator.Bytes()...))

			// not the first time to mint
			if cntBytes != nil {
				cnt = int64(binary.BigEndian.Uint64(cntBytes)) + 1
			}
		}
	}

	newCntBytes := make([]byte, 8)
	newEpochBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(newEpochBytes, uint64(newEpoch))
	binary.BigEndian.PutUint64(newCntBytes, uint64(cnt))
	dposContext.MintCntTrie().TryUpdate(append(newEpochBytes, validator.Bytes()...), newCntBytes)
}
