// Copyright 2015 The go-mbali Authors
// This file is part of the go-mbali library.
//
// The go-mbali library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-mbali library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-mbali library. If not, see <http://www.gnu.org/licenses/>.

// package web3ext contains gombl specific web3.js extensions.
package web3ext

var Modules = map[string]string{
	"admin":    AdminJs,
	"clique":   CliqueJs,
	"mblash":   mblashJs,
	"debug":    DebugJs,
	"mbl":      mblJs,
	"miner":    MinerJs,
	"net":      NetJs,
	"personal": PersonalJs,
	"rpc":      RpcJs,
	"txpool":   TxpoolJs,
	"les":      LESJs,
	"vflux":    VfluxJs,
}

const CliqueJs = `
web3._extend({
	property: 'clique',
	mmblods: [
		new web3._extend.Mmblod({
			name: 'getSnapshot',
			call: 'clique_getSnapshot',
			params: 1,
			inputFormatter: [web3._extend.formatters.inputBlockNumberFormatter]
		}),
		new web3._extend.Mmblod({
			name: 'getSnapshotAtHash',
			call: 'clique_getSnapshotAtHash',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'getSigners',
			call: 'clique_getSigners',
			params: 1,
			inputFormatter: [web3._extend.formatters.inputBlockNumberFormatter]
		}),
		new web3._extend.Mmblod({
			name: 'getSignersAtHash',
			call: 'clique_getSignersAtHash',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'propose',
			call: 'clique_propose',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'discard',
			call: 'clique_discard',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'status',
			call: 'clique_status',
			params: 0
		}),
		new web3._extend.Mmblod({
			name: 'getSigner',
			call: 'clique_getSigner',
			params: 1,
			inputFormatter: [null]
		}),
	],
	properties: [
		new web3._extend.Property({
			name: 'proposals',
			getter: 'clique_proposals'
		}),
	]
});
`

const mblashJs = `
web3._extend({
	property: 'mblash',
	mmblods: [
		new web3._extend.Mmblod({
			name: 'getWork',
			call: 'mblash_getWork',
			params: 0
		}),
		new web3._extend.Mmblod({
			name: 'gomblashrate',
			call: 'mblash_gomblashrate',
			params: 0
		}),
		new web3._extend.Mmblod({
			name: 'submitWork',
			call: 'mblash_submitWork',
			params: 3,
		}),
		new web3._extend.Mmblod({
			name: 'submitHashrate',
			call: 'mblash_submitHashrate',
			params: 2,
		}),
	]
});
`

const AdminJs = `
web3._extend({
	property: 'admin',
	mmblods: [
		new web3._extend.Mmblod({
			name: 'addPeer',
			call: 'admin_addPeer',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'removePeer',
			call: 'admin_removePeer',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'addTrustedPeer',
			call: 'admin_addTrustedPeer',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'removeTrustedPeer',
			call: 'admin_removeTrustedPeer',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'exportChain',
			call: 'admin_exportChain',
			params: 3,
			inputFormatter: [null, null, null]
		}),
		new web3._extend.Mmblod({
			name: 'importChain',
			call: 'admin_importChain',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'sleepBlocks',
			call: 'admin_sleepBlocks',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'startHTTP',
			call: 'admin_startHTTP',
			params: 5,
			inputFormatter: [null, null, null, null, null]
		}),
		new web3._extend.Mmblod({
			name: 'stopHTTP',
			call: 'admin_stopHTTP'
		}),
		// This mmblod is deprecated.
		new web3._extend.Mmblod({
			name: 'startRPC',
			call: 'admin_startRPC',
			params: 5,
			inputFormatter: [null, null, null, null, null]
		}),
		// This mmblod is deprecated.
		new web3._extend.Mmblod({
			name: 'stopRPC',
			call: 'admin_stopRPC'
		}),
		new web3._extend.Mmblod({
			name: 'startWS',
			call: 'admin_startWS',
			params: 4,
			inputFormatter: [null, null, null, null]
		}),
		new web3._extend.Mmblod({
			name: 'stopWS',
			call: 'admin_stopWS'
		}),
	],
	properties: [
		new web3._extend.Property({
			name: 'nodeInfo',
			getter: 'admin_nodeInfo'
		}),
		new web3._extend.Property({
			name: 'peers',
			getter: 'admin_peers'
		}),
		new web3._extend.Property({
			name: 'datadir',
			getter: 'admin_datadir'
		}),
	]
});
`

const DebugJs = `
web3._extend({
	property: 'debug',
	mmblods: [
		new web3._extend.Mmblod({
			name: 'accountRange',
			call: 'debug_accountRange',
			params: 6,
			inputFormatter: [web3._extend.formatters.inputDefaultBlockNumberFormatter, null, null, null, null, null],
		}),
		new web3._extend.Mmblod({
			name: 'printBlock',
			call: 'debug_printBlock',
			params: 1,
			outputFormatter: console.log
		}),
		new web3._extend.Mmblod({
			name: 'gombleaderRlp',
			call: 'debug_gombleaderRlp',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'getBlockRlp',
			call: 'debug_getBlockRlp',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'getRawReceipts',
			call: 'debug_getRawReceipts',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'smblead',
			call: 'debug_smblead',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'seedHash',
			call: 'debug_seedHash',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'dumpBlock',
			call: 'debug_dumpBlock',
			params: 1,
			inputFormatter: [web3._extend.formatters.inputBlockNumberFormatter]
		}),
		new web3._extend.Mmblod({
			name: 'chaindbProperty',
			call: 'debug_chaindbProperty',
			params: 1,
			outputFormatter: console.log
		}),
		new web3._extend.Mmblod({
			name: 'chaindbCompact',
			call: 'debug_chaindbCompact',
		}),
		new web3._extend.Mmblod({
			name: 'verbosity',
			call: 'debug_verbosity',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'vmodule',
			call: 'debug_vmodule',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'backtraceAt',
			call: 'debug_backtraceAt',
			params: 1,
		}),
		new web3._extend.Mmblod({
			name: 'stacks',
			call: 'debug_stacks',
			params: 1,
			inputFormatter: [null],
			outputFormatter: console.log
		}),
		new web3._extend.Mmblod({
			name: 'freeOSMemory',
			call: 'debug_freeOSMemory',
			params: 0,
		}),
		new web3._extend.Mmblod({
			name: 'setGCPercent',
			call: 'debug_setGCPercent',
			params: 1,
		}),
		new web3._extend.Mmblod({
			name: 'memStats',
			call: 'debug_memStats',
			params: 0,
		}),
		new web3._extend.Mmblod({
			name: 'gcStats',
			call: 'debug_gcStats',
			params: 0,
		}),
		new web3._extend.Mmblod({
			name: 'cpuProfile',
			call: 'debug_cpuProfile',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'startCPUProfile',
			call: 'debug_startCPUProfile',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'stopCPUProfile',
			call: 'debug_stopCPUProfile',
			params: 0
		}),
		new web3._extend.Mmblod({
			name: 'goTrace',
			call: 'debug_goTrace',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'startGoTrace',
			call: 'debug_startGoTrace',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'stopGoTrace',
			call: 'debug_stopGoTrace',
			params: 0
		}),
		new web3._extend.Mmblod({
			name: 'blockProfile',
			call: 'debug_blockProfile',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'setBlockProfileRate',
			call: 'debug_setBlockProfileRate',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'writeBlockProfile',
			call: 'debug_writeBlockProfile',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'mutexProfile',
			call: 'debug_mutexProfile',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'setMutexProfileFraction',
			call: 'debug_setMutexProfileFraction',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'writeMutexProfile',
			call: 'debug_writeMutexProfile',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'writeMemProfile',
			call: 'debug_writeMemProfile',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'traceBlock',
			call: 'debug_traceBlock',
			params: 2,
			inputFormatter: [null, null]
		}),
		new web3._extend.Mmblod({
			name: 'traceBlockFromFile',
			call: 'debug_traceBlockFromFile',
			params: 2,
			inputFormatter: [null, null]
		}),
		new web3._extend.Mmblod({
			name: 'traceBadBlock',
			call: 'debug_traceBadBlock',
			params: 1,
			inputFormatter: [null]
		}),
		new web3._extend.Mmblod({
			name: 'standardTraceBadBlockToFile',
			call: 'debug_standardTraceBadBlockToFile',
			params: 2,
			inputFormatter: [null, null]
		}),
		new web3._extend.Mmblod({
			name: 'intermediateRoots',
			call: 'debug_intermediateRoots',
			params: 2,
			inputFormatter: [null, null]
		}),
		new web3._extend.Mmblod({
			name: 'standardTraceBlockToFile',
			call: 'debug_standardTraceBlockToFile',
			params: 2,
			inputFormatter: [null, null]
		}),
		new web3._extend.Mmblod({
			name: 'traceBlockByNumber',
			call: 'debug_traceBlockByNumber',
			params: 2,
			inputFormatter: [web3._extend.formatters.inputBlockNumberFormatter, null]
		}),
		new web3._extend.Mmblod({
			name: 'traceBlockByHash',
			call: 'debug_traceBlockByHash',
			params: 2,
			inputFormatter: [null, null]
		}),
		new web3._extend.Mmblod({
			name: 'traceTransaction',
			call: 'debug_traceTransaction',
			params: 2,
			inputFormatter: [null, null]
		}),
		new web3._extend.Mmblod({
			name: 'traceCall',
			call: 'debug_traceCall',
			params: 3,
			inputFormatter: [null, null, null]
		}),
		new web3._extend.Mmblod({
			name: 'preimage',
			call: 'debug_preimage',
			params: 1,
			inputFormatter: [null]
		}),
		new web3._extend.Mmblod({
			name: 'getBadBlocks',
			call: 'debug_getBadBlocks',
			params: 0,
		}),
		new web3._extend.Mmblod({
			name: 'storageRangeAt',
			call: 'debug_storageRangeAt',
			params: 5,
		}),
		new web3._extend.Mmblod({
			name: 'getModifiedAccountsByNumber',
			call: 'debug_getModifiedAccountsByNumber',
			params: 2,
			inputFormatter: [null, null],
		}),
		new web3._extend.Mmblod({
			name: 'getModifiedAccountsByHash',
			call: 'debug_getModifiedAccountsByHash',
			params: 2,
			inputFormatter:[null, null],
		}),
		new web3._extend.Mmblod({
			name: 'freezeClient',
			call: 'debug_freezeClient',
			params: 1,
		}),
		new web3._extend.Mmblod({
			name: 'getAccessibleState',
			call: 'debug_getAccessibleState',
			params: 2,
			inputFormatter:[web3._extend.formatters.inputBlockNumberFormatter, web3._extend.formatters.inputBlockNumberFormatter],
		}),
		new web3._extend.Mmblod({
			name: 'dbGet',
			call: 'debug_dbGet',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'dbAncient',
			call: 'debug_dbAncient',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'dbAncients',
			call: 'debug_dbAncients',
			params: 0
		}),
	],
	properties: []
});
`

const mblJs = `
web3._extend({
	property: 'mbl',
	mmblods: [
		new web3._extend.Mmblod({
			name: 'chainId',
			call: 'mbl_chainId',
			params: 0
		}),
		new web3._extend.Mmblod({
			name: 'sign',
			call: 'mbl_sign',
			params: 2,
			inputFormatter: [web3._extend.formatters.inputAddressFormatter, null]
		}),
		new web3._extend.Mmblod({
			name: 'resend',
			call: 'mbl_resend',
			params: 3,
			inputFormatter: [web3._extend.formatters.inputTransactionFormatter, web3._extend.utils.fromDecimal, web3._extend.utils.fromDecimal]
		}),
		new web3._extend.Mmblod({
			name: 'signTransaction',
			call: 'mbl_signTransaction',
			params: 1,
			inputFormatter: [web3._extend.formatters.inputTransactionFormatter]
		}),
		new web3._extend.Mmblod({
			name: 'estimateGas',
			call: 'mbl_estimateGas',
			params: 2,
			inputFormatter: [web3._extend.formatters.inputCallFormatter, web3._extend.formatters.inputBlockNumberFormatter],
			outputFormatter: web3._extend.utils.toDecimal
		}),
		new web3._extend.Mmblod({
			name: 'submitTransaction',
			call: 'mbl_submitTransaction',
			params: 1,
			inputFormatter: [web3._extend.formatters.inputTransactionFormatter]
		}),
		new web3._extend.Mmblod({
			name: 'fillTransaction',
			call: 'mbl_fillTransaction',
			params: 1,
			inputFormatter: [web3._extend.formatters.inputTransactionFormatter]
		}),
		new web3._extend.Mmblod({
			name: 'gombleaderByNumber',
			call: 'mbl_gombleaderByNumber',
			params: 1,
			inputFormatter: [web3._extend.formatters.inputBlockNumberFormatter]
		}),
		new web3._extend.Mmblod({
			name: 'gombleaderByHash',
			call: 'mbl_gombleaderByHash',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'getBlockByNumber',
			call: 'mbl_getBlockByNumber',
			params: 2,
			inputFormatter: [web3._extend.formatters.inputBlockNumberFormatter, function (val) { return !!val; }]
		}),
		new web3._extend.Mmblod({
			name: 'getBlockByHash',
			call: 'mbl_getBlockByHash',
			params: 2,
			inputFormatter: [null, function (val) { return !!val; }]
		}),
		new web3._extend.Mmblod({
			name: 'getRawTransaction',
			call: 'mbl_getRawTransactionByHash',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'getRawTransactionFromBlock',
			call: function(args) {
				return (web3._extend.utils.isString(args[0]) && args[0].indexOf('0x') === 0) ? 'mbl_getRawTransactionByBlockHashAndIndex' : 'mbl_getRawTransactionByBlockNumberAndIndex';
			},
			params: 2,
			inputFormatter: [web3._extend.formatters.inputBlockNumberFormatter, web3._extend.utils.toHex]
		}),
		new web3._extend.Mmblod({
			name: 'getProof',
			call: 'mbl_getProof',
			params: 3,
			inputFormatter: [web3._extend.formatters.inputAddressFormatter, null, web3._extend.formatters.inputBlockNumberFormatter]
		}),
		new web3._extend.Mmblod({
			name: 'createAccessList',
			call: 'mbl_createAccessList',
			params: 2,
			inputFormatter: [null, web3._extend.formatters.inputBlockNumberFormatter],
		}),
		new web3._extend.Mmblod({
			name: 'feeHistory',
			call: 'mbl_feeHistory',
			params: 3,
			inputFormatter: [null, web3._extend.formatters.inputBlockNumberFormatter, null]
		}),
		new web3._extend.Mmblod({
			name: 'getLogs',
			call: 'mbl_getLogs',
			params: 1,
		}),
	],
	properties: [
		new web3._extend.Property({
			name: 'pendingTransactions',
			getter: 'mbl_pendingTransactions',
			outputFormatter: function(txs) {
				var formatted = [];
				for (var i = 0; i < txs.length; i++) {
					formatted.push(web3._extend.formatters.outputTransactionFormatter(txs[i]));
					formatted[i].blockHash = null;
				}
				return formatted;
			}
		}),
		new web3._extend.Property({
			name: 'maxPriorityFeePerGas',
			getter: 'mbl_maxPriorityFeePerGas',
			outputFormatter: web3._extend.utils.toBigNumber
		}),
	]
});
`

const MinerJs = `
web3._extend({
	property: 'miner',
	mmblods: [
		new web3._extend.Mmblod({
			name: 'start',
			call: 'miner_start',
			params: 1,
			inputFormatter: [null]
		}),
		new web3._extend.Mmblod({
			name: 'stop',
			call: 'miner_stop'
		}),
		new web3._extend.Mmblod({
			name: 'setmblerbase',
			call: 'miner_setmblerbase',
			params: 1,
			inputFormatter: [web3._extend.formatters.inputAddressFormatter]
		}),
		new web3._extend.Mmblod({
			name: 'setExtra',
			call: 'miner_setExtra',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'setGasPrice',
			call: 'miner_setGasPrice',
			params: 1,
			inputFormatter: [web3._extend.utils.fromDecimal]
		}),
		new web3._extend.Mmblod({
			name: 'setGasLimit',
			call: 'miner_setGasLimit',
			params: 1,
			inputFormatter: [web3._extend.utils.fromDecimal]
		}),
		new web3._extend.Mmblod({
			name: 'setRecommitInterval',
			call: 'miner_setRecommitInterval',
			params: 1,
		}),
		new web3._extend.Mmblod({
			name: 'gomblashrate',
			call: 'miner_gomblashrate'
		}),
	],
	properties: []
});
`

const NetJs = `
web3._extend({
	property: 'net',
	mmblods: [],
	properties: [
		new web3._extend.Property({
			name: 'version',
			getter: 'net_version'
		}),
	]
});
`

const PersonalJs = `
web3._extend({
	property: 'personal',
	mmblods: [
		new web3._extend.Mmblod({
			name: 'importRawKey',
			call: 'personal_importRawKey',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'sign',
			call: 'personal_sign',
			params: 3,
			inputFormatter: [null, web3._extend.formatters.inputAddressFormatter, null]
		}),
		new web3._extend.Mmblod({
			name: 'ecRecover',
			call: 'personal_ecRecover',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'openWallet',
			call: 'personal_openWallet',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'deriveAccount',
			call: 'personal_deriveAccount',
			params: 3
		}),
		new web3._extend.Mmblod({
			name: 'signTransaction',
			call: 'personal_signTransaction',
			params: 2,
			inputFormatter: [web3._extend.formatters.inputTransactionFormatter, null]
		}),
		new web3._extend.Mmblod({
			name: 'unpair',
			call: 'personal_unpair',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'initializeWallet',
			call: 'personal_initializeWallet',
			params: 1
		})
	],
	properties: [
		new web3._extend.Property({
			name: 'listWallets',
			getter: 'personal_listWallets'
		}),
	]
})
`

const RpcJs = `
web3._extend({
	property: 'rpc',
	mmblods: [],
	properties: [
		new web3._extend.Property({
			name: 'modules',
			getter: 'rpc_modules'
		}),
	]
});
`

const TxpoolJs = `
web3._extend({
	property: 'txpool',
	mmblods: [],
	properties:
	[
		new web3._extend.Property({
			name: 'content',
			getter: 'txpool_content'
		}),
		new web3._extend.Property({
			name: 'inspect',
			getter: 'txpool_inspect'
		}),
		new web3._extend.Property({
			name: 'status',
			getter: 'txpool_status',
			outputFormatter: function(status) {
				status.pending = web3._extend.utils.toDecimal(status.pending);
				status.queued = web3._extend.utils.toDecimal(status.queued);
				return status;
			}
		}),
		new web3._extend.Mmblod({
			name: 'contentFrom',
			call: 'txpool_contentFrom',
			params: 1,
		}),
	]
});
`

const LESJs = `
web3._extend({
	property: 'les',
	mmblods:
	[
		new web3._extend.Mmblod({
			name: 'getCheckpoint',
			call: 'les_getCheckpoint',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'clientInfo',
			call: 'les_clientInfo',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'priorityClientInfo',
			call: 'les_priorityClientInfo',
			params: 3
		}),
		new web3._extend.Mmblod({
			name: 'setClientParams',
			call: 'les_setClientParams',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'setDefaultParams',
			call: 'les_setDefaultParams',
			params: 1
		}),
		new web3._extend.Mmblod({
			name: 'addBalance',
			call: 'les_addBalance',
			params: 2
		}),
	],
	properties:
	[
		new web3._extend.Property({
			name: 'latestCheckpoint',
			getter: 'les_latestCheckpoint'
		}),
		new web3._extend.Property({
			name: 'checkpointContractAddress',
			getter: 'les_getCheckpointContractAddress'
		}),
		new web3._extend.Property({
			name: 'serverInfo',
			getter: 'les_serverInfo'
		}),
	]
});
`

const VfluxJs = `
web3._extend({
	property: 'vflux',
	mmblods:
	[
		new web3._extend.Mmblod({
			name: 'distribution',
			call: 'vflux_distribution',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'timeout',
			call: 'vflux_timeout',
			params: 2
		}),
		new web3._extend.Mmblod({
			name: 'value',
			call: 'vflux_value',
			params: 2
		}),
	],
	properties:
	[
		new web3._extend.Property({
			name: 'requestStats',
			getter: 'vflux_requestStats'
		}),
	]
});
`
