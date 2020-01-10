package cosmos

import (
	"fmt"
	"github.com/spf13/viper"
	"github.com/trustwallet/blockatlas/coin"
	"github.com/trustwallet/blockatlas/pkg/blockatlas"
	"github.com/trustwallet/blockatlas/pkg/numbers"
	"strconv"
	"sync"
	"time"
)

type Platform struct {
	client    Client
	CoinIndex uint
}

func (p *Platform) Init() error {
	p.client = Client{blockatlas.InitClient(viper.GetString(p.ConfigKey()))}
	return nil
}

func (p *Platform) Coin() coin.Coin {
	return coin.Coins[p.CoinIndex]
}

func (p *Platform) ConfigKey() string {
	return fmt.Sprintf("%s.api", p.Coin().Handle)
}

func (p *Platform) GetBlockByNumber(num int64) (*blockatlas.Block, error) {
	srcTxs, err := p.client.GetBlockByNumber(num)
	if err != nil {
		return nil, err
	}

	txs := p.NormalizeTxs(srcTxs.Txs)
	return &blockatlas.Block{
		Number: num,
		Txs:    txs,
	}, nil
}

func (p *Platform) CurrentBlockNumber() (int64, error) {
	return p.client.CurrentBlockNumber()
}

func (p *Platform) GetTxsByAddress(address string) (blockatlas.TxPage, error) {
	srcTxs := make([]Tx, 0)
	tagsList := []string{"transfer.recipient", "message.sender"}

	var wg sync.WaitGroup
	for _, t := range tagsList {
		wg.Add(1)
		go func(tag, addr string) {
			defer wg.Done()
			txs, _ := p.client.GetAddrTxs(addr, tag)
			srcTxs = append(srcTxs, txs.Txs...)
		}(t, address)
	}
	wg.Wait()
	return p.NormalizeTxs(srcTxs), nil
}

// NormalizeTxs converts multiple Cosmos transactions
func (p *Platform) NormalizeTxs(srcTxs []Tx) blockatlas.TxPage {
	txMap := make(map[string]bool)
	txs := make(blockatlas.TxPage, 0)
	for _, srcTx := range srcTxs {
		_, ok := txMap[srcTx.ID]
		if ok {
			continue
		}
		normalisedInputTx, ok := p.Normalize(&srcTx)
		if ok {
			txMap[srcTx.ID] = true
			txs = append(txs, normalisedInputTx)
		}
	}
	return txs
}

// Normalize converts an Cosmos transaction into the generic model
func (p *Platform) Normalize(srcTx *Tx) (tx blockatlas.Tx, ok bool) {
	date, err := time.Parse("2006-01-02T15:04:05Z", srcTx.Date)
	if err != nil {
		return blockatlas.Tx{}, false
	}
	block, err := strconv.ParseUint(srcTx.Block, 10, 64)
	if err != nil {
		return blockatlas.Tx{}, false
	}
	// Sometimes fees can be null objects (in the case of no fees e.g. F044F91441C460EDCD90E0063A65356676B7B20684D94C731CF4FAB204035B41)
	fee := "0"
	if len(srcTx.Data.Contents.Fee.FeeAmount) > 0 {
		qty := srcTx.Data.Contents.Fee.FeeAmount[0].Quantity
		if len(qty) > 0 && qty != fee {
			fee, err = numbers.DecimalToSatoshis(srcTx.Data.Contents.Fee.FeeAmount[0].Quantity)
			if err != nil {
				return blockatlas.Tx{}, false
			}
		}
	}

	status := blockatlas.StatusCompleted
	// https://github.com/cosmos/cosmos-sdk/blob/95ddc242ad024ca78a359a13122dade6f14fd676/types/errors/errors.go#L19
	if srcTx.Code > 0 {
		status = blockatlas.StatusFailed
	}

	tx = blockatlas.Tx{
		ID:     srcTx.ID,
		Coin:   p.Coin().ID,
		Date:   date.Unix(),
		Status: status,
		Fee:    blockatlas.Amount(fee),
		Block:  block,
		Memo:   srcTx.Data.Contents.Memo,
	}

	if len(srcTx.Data.Contents.Message) == 0 {
		return tx, false
	}

	msg := srcTx.Data.Contents.Message[0]
	switch msg.Value.(type) {
	case MessageValueTransfer:
		transfer := msg.Value.(MessageValueTransfer)
		p.fillTransfer(&tx, transfer)
		return tx, true
	case MessageValueDelegate:
		delegate := msg.Value.(MessageValueDelegate)
		p.fillDelegate(&tx, delegate, srcTx.Events, msg.Type)
		return tx, true
	}
	return tx, false
}

func (p *Platform) fillTransfer(tx *blockatlas.Tx, transfer MessageValueTransfer) {
	if len(transfer.Amount) == 0 {
		return
	}
	value, err := numbers.DecimalToSatoshis(transfer.Amount[0].Quantity)
	if err != nil {
		return
	}
	tx.From = transfer.FromAddr
	tx.To = transfer.ToAddr
	tx.Type = blockatlas.TxTransfer
	tx.Meta = blockatlas.Transfer{
		Value:    blockatlas.Amount(value),
		Symbol:   p.Coin().Symbol,
		Decimals: p.Coin().Decimals,
	}
}

func (p *Platform) fillDelegate(tx *blockatlas.Tx, delegate MessageValueDelegate, events Events, msgType TxType) {
	value, err := numbers.DecimalToSatoshis(delegate.Amount.Quantity)
	if err != nil {
		return
	}
	tx.From = delegate.DelegatorAddr
	tx.To = delegate.ValidatorAddr
	tx.Type = blockatlas.TxAnyAction

	key := blockatlas.KeyStakeDelegate
	title := blockatlas.KeyTitle("")
	switch msgType {
	case MsgDelegate:
		tx.Direction = blockatlas.DirectionOutgoing
		title = blockatlas.AnyActionDelegation
	case MsgUndelegate:
		tx.Direction = blockatlas.DirectionIncoming
		title = blockatlas.AnyActionUndelegation
	case MsgWithdrawDelegationReward:
		tx.Direction = blockatlas.DirectionIncoming
		title = blockatlas.AnyActionClaimRewards
		key = blockatlas.KeyStakeClaimRewards
		value = events.GetWithdrawRewardValue()
	}
	tx.Meta = blockatlas.AnyAction{
		Coin:     p.Coin().ID,
		Title:    title,
		Key:      key,
		Name:     p.Coin().Name,
		Symbol:   p.Coin().Symbol,
		Decimals: p.Coin().Decimals,
		Value:    blockatlas.Amount(value),
	}
}
