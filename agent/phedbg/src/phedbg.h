// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// phedbg.h -- an explicit, visible on-device debug agent for stock Nokia Symbian^3-family phones.
// Serves the "phedbg line protocol v1" (pdk/docs/nokia.md) over USB CDC ACM (RComm "ACM::n") and/or TCP, for PhoneLab.
#ifndef PHEDBG_H
#define PHEDBG_H

#include <e32base.h>
#include <f32file.h>
#include <badesca.h>
#include <c32comm.h>
#include <es_sock.h>
#include <in_sock.h>

const TInt KPheDbgVersionMajor = 1;
const TInt KPheDbgVersionMinor = 0;
const TInt KPheDbgDataChunk = 3072;		// raw bytes per base64 "d" line (4096 chars)
const TInt KPheDbgMaxLine = 8192;
const TInt KPheDbgLogLines = 200;

// ---- settings (phedbg.ini in the private dir; plain "key=value" lines)
class TPheDbgSettings
	{
public:
	enum { EUsb = 1, ETcp = 2 };
	void SetDefaults();
	TInt iServer;		// 0/1: start the server when the app starts? (always 0 on load: start is explicit)
	TInt iTransport;	// EUsb | ETcp bits
	TInt iAcmUnit;
	TInt iTcpPort;
	TInt iTcpLan;		// 0 = 127.0.0.1 only, 1 = all interfaces
	TBuf<16> iCode;
	TInt iOpRead, iOpWrite, iOpExec, iOpShot, iOpLog, iOpInput;
	TInt iConfirm;
	TInt iKeepAwake;
	};

class MPheDbgUi
	{
public:
	virtual void StatusChanged() = 0;
	virtual TBool ConfirmL(const TDesC& aWhat) = 0;		// a phone-side Yes/No query
	};

class CPheDbgServer;

// One connection (an ACM port or one TCP client). Lines in, lines out.
class CPheDbgChannel : public CActive
	{
public:
	~CPheDbgChannel();
	virtual void WriteL(const TDesC8& aLine) = 0;		// aLine without the '\n'
	virtual const TDesC& Name() const = 0;
	TBool iConnected;
protected:
	CPheDbgChannel(CPheDbgServer& aServer);
	void Received(const TDesC8& aData);
	CPheDbgServer& iServer;
	TBuf8<512> iRead;
	RBuf8 iLine;
public:
	// a streaming fs_write in progress on this channel
	TUint iWriteId;
	TInt iWriteLeft;
	RFile iWriteFile;
	TBool iWriteOpen;
	};

// State shared by the UI thread and the ACM worker thread (one process heap; guarded by iLock).
struct TPheDbgAcmShared
	{
	RMutex iLock;
	TInt iUnit;
	RBuf8 iIn;						// worker -> UI: received bytes
	RBuf8 iOut;						// UI -> worker: lines to send
	TInt iState;					// 0 = starting, 1 = up, < 0 = failed or gone (error code)
	TBool iStateNew;				// iState changed since the UI last looked
	TThreadId iUiThread;
	TRequestStatus* iUiStatus;		// the UI's armed iStatus, NULL when not armed
	TRequestStatus* iWorkerWake;	// the worker's "outbox or stop" status, NULL when not waiting
	TBool iStop;
	};

// USB CDC ACM. Every C32 call (RCommServ::Connect, LoadCommModule, RComm::Open/Read/Write) can block for a long
// time -- with the PC suite connected, Connect/LoadCommModule do not return -- so they all run in a worker thread.
// The UI thread only swaps buffers with it under a mutex and is never blocked.
class CPheDbgAcm : public CPheDbgChannel
	{
public:
	static CPheDbgAcm* NewL(CPheDbgServer& aServer, TInt aUnit);
	~CPheDbgAcm();
	void WriteL(const TDesC8& aLine);
	const TDesC& Name() const { return iName; }
private:
	CPheDbgAcm(CPheDbgServer& aServer) : CPheDbgChannel(aServer) {}
	void ConstructL(TInt aUnit);
	void Arm();
	void RunL();
	TInt RunError(TInt aErr);
	void DoCancel();
	TPheDbgAcmShared* iShared;
	RThread iWorker;
	TBool iWorkerStarted;
	TBuf<16> iName;
	};

class CPheDbgTcp : public CPheDbgChannel
	{
public:
	static CPheDbgTcp* NewL(CPheDbgServer& aServer, TInt aPort, TBool aLan);
	~CPheDbgTcp();
	void WriteL(const TDesC8& aLine);
	const TDesC& Name() const { return iName; }
private:
	CPheDbgTcp(CPheDbgServer& aServer) : CPheDbgChannel(aServer) {}
	void ConstructL(TInt aPort, TBool aLan);
	void Accept();
	void Issue();
	TInt Listen();
	void Relisten();
	void RunL();
	TInt RunError(TInt aErr);
	void DoCancel();
	RSocketServ iSs;
	RSocket iListen;
	RSocket iSock;
	RTimer iTimer;			// re-listen delay after the network went away
	TBool iSockOpen;
	TBool iListenOpen;
	TBool iAccepting;
	TBool iWaiting;
	TInt iFails;
	TInt iPort;
	TBool iLan;
	TSockXfrLength iLen;
	TBuf<24> iName;
	};

class CPheDbgServer : public CBase
	{
public:
	static CPheDbgServer* NewL(TPheDbgSettings& aSettings, MPheDbgUi& aUi, RFs& aFs);
	~CPheDbgServer();
	void StartL();
	void Stop();
	TBool Running() const { return iRunning; }
	const TDesC& StatusText() const { return iStatus; }
	void Log(const TDesC& aText);
	const CDesCArray& LogLines() const { return *iLog; }
	CDesCArray& AllowList() { return *iAllow; }
	void LoadAllowListL();
	void SaveAllowListL();
	TInt iRequests;
	// called by channels
	void HandleLineL(CPheDbgChannel& aCh, const TDesC8& aLine);
	void ChannelUp(CPheDbgChannel& aCh);
	void ChannelDown(CPheDbgChannel& aCh, TInt aErr);
private:
	CPheDbgServer(TPheDbgSettings& aSettings, MPheDbgUi& aUi, RFs& aFs) : iSettings(aSettings), iUi(aUi), iFs(aFs) {}
	void ConstructL();
	void UpdateStatus();
	// ops
	void OpInfoL(CPheDbgChannel& aCh, const TDesC8& aId);
	void OpListL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath);
	void OpReadL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath);
	void OpWriteL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath, TInt aSize);
	void OpWriteDataL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC8& aB64);
	void OpDeleteL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath);
	void OpExecL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath, const TDesC& aArgs, TBool aWait);
	void OpPsL(CPheDbgChannel& aCh, const TDesC8& aId);
	void OpShotL(CPheDbgChannel& aCh, const TDesC8& aId);
	void OpLogL(CPheDbgChannel& aCh, const TDesC8& aId);
	// helpers
	void SendDataL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC8& aData);
	void ReplyErrL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC8& aCode, const TDesC& aMsg);
	void ReplyOkL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC8& aRest);
	TBool AllowedWrite(const TDesC& aPath) const;
	TPheDbgSettings& iSettings;
	MPheDbgUi& iUi;
	RFs& iFs;
	CDesCArray* iLog;
	CDesCArray* iAllow;
	RPointerArray<CPheDbgChannel> iChannels;
	TBool iRunning;
	TBuf<64> iStatus;
	};

// ---- the protocol helpers (phedbg_proto.cpp)
namespace PheDbgProto
	{
	void Base64EncodeL(const TDesC8& aIn, RBuf8& aOut);		// appends
	TInt Base64Decode(const TDesC8& aIn, TDes8& aOut);
	void PercentEncodeL(const TDesC& aIn, RBuf8& aOut);		// UTF-8 + %XX; appends
	void PercentDecodeL(const TDesC8& aIn, RBuf& aOut);		// %XX + UTF-8 -> UTF-16
	TBool Field(const TDesC8& aLine, const TDesC8& aKey, TPtrC8& aValue);	// "k=v" among space-separated fields
	}

#endif
