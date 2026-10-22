// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// phedbg_server.cpp -- the request handler of the phedbg line protocol v1 (pdk/docs/nokia.md), and its two channels:
// USB CDC ACM through RComm, and TCP through RSocket. Every request is checked in this order: pairing code, the op's
// on/off switch in settings, the write allow-list (writes and deletes), then the optional on-phone confirmation.
// Every request and its verdict goes into the session log that the app shows.
#include "phedbg.h"
#include "phedbg_caps.h"
#include <hal.h>
#include <e32svr.h>
#include <coemain.h>
#include <w32std.h>
#include <fbs.h>

_LIT(KPheDbgPrivAllow, "c:\\data\\phedbg\\");
_LIT8(KTok, "tok");
_LIT8(KPath, "path");

// ---------------------------------------------------------------- settings

void TPheDbgSettings::SetDefaults()
	{
	iServer = 0;
	iTransport = EUsb;
	iAcmUnit = 1;
	iTcpPort = 7654;
	iTcpLan = 0;
	iCode.Zero();
	iOpRead = iOpWrite = iOpExec = iOpShot = iOpLog = 1;
	iOpInput = 0;
	iConfirm = 1;
	iKeepAwake = 1;
	}

// ---------------------------------------------------------------- channel base

CPheDbgChannel::CPheDbgChannel(CPheDbgServer& aServer)
	: CActive(EPriorityStandard), iServer(aServer)
	{
	CActiveScheduler::Add(this);
	}

CPheDbgChannel::~CPheDbgChannel()
	{
	if (iWriteOpen)
		iWriteFile.Close();
	iLine.Close();
	}

// Split the received bytes into lines; hand each complete line to the server.
void CPheDbgChannel::Received(const TDesC8& aData)
	{
	for (TInt i = 0; i < aData.Length(); i++)
		{
		TUint8 c = aData[i];
		if (c == '\n')
			{
			if (iLine.Length() && iLine[iLine.Length() - 1] == '\r')
				iLine.SetLength(iLine.Length() - 1);
			TRAPD(err, iServer.HandleLineL(*this, iLine));
			if (err != KErrNone)
				iServer.Log(_L("request failed (internal error)"));
			iLine.Zero();
			}
		else if (iLine.Length() < KPheDbgMaxLine)
			{
			if (iLine.Length() == iLine.MaxLength())
				{
				if (iLine.ReAlloc(Min(iLine.MaxLength() * 2 + 256, KPheDbgMaxLine)) != KErrNone)
					continue;
				}
			iLine.Append(c);
			}
		// an over-long line is truncated; it then fails to parse and gets an error reply
		}
	}

// ---------------------------------------------------------------- USB ACM channel

CPheDbgAcm* CPheDbgAcm::NewL(CPheDbgServer& aServer, TInt aUnit)
	{
	CPheDbgAcm* self = new (ELeave) CPheDbgAcm(aServer);
	CleanupStack::PushL(self);
	self->ConstructL(aUnit);
	CleanupStack::Pop(self);
	return self;
	}

// Called with iLock held: complete the UI's armed status, if any.
static void AcmKickUi(TPheDbgAcmShared& aSh)
	{
	if (!aSh.iUiStatus)
		return;
	RThread ui;
	if (ui.Open(aSh.iUiThread) == KErrNone)
		{
		ui.RequestComplete(aSh.iUiStatus, KErrNone);
		ui.Close();
		}
	aSh.iUiStatus = NULL;
	}

static void AcmSetState(TPheDbgAcmShared& aSh, TInt aState)
	{
	aSh.iLock.Wait();
	aSh.iState = aState;
	aSh.iStateNew = ETrue;
	AcmKickUi(aSh);
	aSh.iLock.Signal();
	}

// Write with a time limit: a host that does not read (no terminal open) must not wedge the worker for ever.
static TInt AcmTimedWrite(RComm& aComm, RTimer& aTimer, const TDesC8& aData)
	{
	TRequestStatus wst, tst;
	aComm.Write(wst, aData);
	aTimer.After(tst, 5000000);
	User::WaitForRequest(wst, tst);
	if (wst == KRequestPending)
		{
		aComm.WriteCancel();
		User::WaitForRequest(wst);
		return KErrTimedOut;
		}
	aTimer.Cancel();
	User::WaitForRequest(tst);
	return wst.Int();
	}

static TInt AcmWorkerRun(TPheDbgAcmShared& aSh)
	{
	RCommServ cs;
	TInt err = cs.Connect();
	if (err != KErrNone)
		return err;
	err = cs.LoadCommModule(_L("ECACM"));
	if (err != KErrNone && err != KErrAlreadyExists)
		{
		cs.Close();
		return err;
		}
	TSerialInfo si;
	err = cs.GetPortInfo(_L("ACM"), si);
	if (err == KErrNone && ((TUint)aSh.iUnit < si.iLowUnit || (TUint)aSh.iUnit > si.iHighUnit))
		err = KErrNotFound;		// e.g. the phone is in mass storage / media transfer mode: no ACM ports
	RComm comm;
	TBuf<16> name;
	name.Format(_L("ACM::%d"), aSh.iUnit);
	if (err == KErrNone)
		err = comm.Open(cs, name, ECommShared);
	RTimer timer;
	if (err == KErrNone)
		err = timer.CreateLocal();
	if (err != KErrNone)
		{
		comm.Close();
		cs.Close();
		return err;
		}
	AcmSetState(aSh, 1);

	TBuf8<512> rd;
	RBuf8 out;
	TRequestStatus readSt, wakeSt;
	TInt readErrors = 0;
	comm.ReadOneOrMore(readSt, rd);
	wakeSt = KRequestPending;
	aSh.iLock.Wait();
	aSh.iWorkerWake = &wakeSt;
	TBool pending = aSh.iOut.Length() > 0 || aSh.iStop;
	aSh.iLock.Signal();
	if (pending)
		{
		aSh.iLock.Wait();
		if (aSh.iWorkerWake)
			{
			aSh.iWorkerWake = NULL;
			TRequestStatus* s = &wakeSt;
			User::RequestComplete(s, KErrNone);
			}
		aSh.iLock.Signal();
		}

	err = KErrNone;
	for (;;)
		{
		User::WaitForAnyRequest();
		if (wakeSt != KRequestPending)
			{
			aSh.iLock.Wait();
			TBool stop = aSh.iStop;
			if (!stop && aSh.iOut.Length())
				{
				out.Close();
				out.Swap(aSh.iOut);		// take the whole outbox; the UI side gets a fresh, empty buffer
				}
			wakeSt = KRequestPending;
			aSh.iWorkerWake = stop ? NULL : &wakeSt;
			aSh.iLock.Signal();
			if (stop)
				break;
			if (out.Length())
				AcmTimedWrite(comm, timer, out);	// a timed-out reply is dropped: nobody was reading
			out.Zero();
			}
		if (readSt != KRequestPending)
			{
			if (readSt.Int() == KErrNone)
				{
				readErrors = 0;
				aSh.iLock.Wait();
				if (aSh.iIn.Length() + rd.Length() > aSh.iIn.MaxLength())
					aSh.iIn.ReAlloc(aSh.iIn.Length() + rd.Length() + 512);
				if (aSh.iIn.Length() + rd.Length() <= aSh.iIn.MaxLength())
					aSh.iIn.Append(rd);
				AcmKickUi(aSh);
				aSh.iLock.Signal();
				}
			else if (++readErrors > 20)
				{
				err = readSt.Int();		// cable gone or the port was closed for good
				break;
				}
			else
				User::After(200000);	// the host closed the port: wait a little and read again (worker only)
			rd.Zero();
			comm.ReadOneOrMore(readSt, rd);
			}
		}

	if (readSt == KRequestPending)
		{
		comm.ReadCancel();
		User::WaitForRequest(readSt);
		}
	aSh.iLock.Wait();
	TBool wakeArmed = aSh.iWorkerWake != NULL;
	aSh.iWorkerWake = NULL;
	aSh.iLock.Signal();
	if (wakeArmed)
		{
		TRequestStatus* s = &wakeSt;	// nobody will complete it now; balance it ourselves
		User::RequestComplete(s, KErrCancel);
		User::WaitForRequest(wakeSt);
		}
	out.Close();
	timer.Close();
	comm.Close();
	cs.Close();
	return err;
	}

static TInt AcmWorker(TAny* aShared)
	{
	TPheDbgAcmShared& sh = *static_cast<TPheDbgAcmShared*>(aShared);
	TInt err = AcmWorkerRun(sh);
	AcmSetState(sh, err < 0 ? err : KErrDisconnected);
	return err;
	}

void CPheDbgAcm::ConstructL(TInt aUnit)
	{
	iLine.CreateL(256);
	iName.Format(_L("ACM::%d"), aUnit);
	iShared = new (ELeave) TPheDbgAcmShared;
	Mem::FillZ(iShared, sizeof(TPheDbgAcmShared));
	iShared->iUnit = aUnit;
	iShared->iUiThread = RThread().Id();
	User::LeaveIfError(iShared->iLock.CreateLocal(EOwnerProcess));
	iShared->iIn.CreateL(1024);
	iShared->iOut.CreateL(1024);
	TName tn;
	tn.Format(_L("phedbg-acm%d-%x"), aUnit, (TUint)User::NTickCount());
	// NULL heap = share this thread's heap, so the RBuf8s can be grown and freed on either side
	User::LeaveIfError(iWorker.Create(tn, AcmWorker, 0x2000, NULL, iShared, EOwnerProcess));
	iWorkerStarted = ETrue;
	Arm();
	iWorker.Resume();
	}

CPheDbgAcm::~CPheDbgAcm()
	{
	Cancel();
	if (iWorkerStarted)
		{
		TRequestStatus dead;
		iWorker.Logon(dead);
		iShared->iLock.Wait();
		iShared->iStop = ETrue;
		if (iShared->iWorkerWake)
			{
			iWorker.RequestComplete(iShared->iWorkerWake, KErrCancel);
			iShared->iWorkerWake = NULL;
			}
		iShared->iLock.Signal();
		// give a worker that is idle up to 2 s to clean up; one still stuck inside C32 is killed
		RTimer t;
		if (dead == KRequestPending && t.CreateLocal() == KErrNone)
			{
			TRequestStatus ts;
			t.After(ts, 2000000);
			User::WaitForRequest(dead, ts);
			if (ts == KRequestPending)
				{
				t.Cancel();
				User::WaitForRequest(ts);
				}
			t.Close();
			}
		if (dead == KRequestPending)
			{
			iWorker.Kill(KErrCancel);
			User::WaitForRequest(dead);
			}
		iWorker.Close();
		}
	if (iShared)
		{
		iShared->iIn.Close();
		iShared->iOut.Close();
		iShared->iLock.Close();
		delete iShared;
		}
	}

// Arm iStatus for the worker; complete it at once if something is already waiting.
void CPheDbgAcm::Arm()
	{
	iStatus = KRequestPending;
	SetActive();
	iShared->iLock.Wait();
	if (iShared->iIn.Length() || iShared->iStateNew)
		{
		TRequestStatus* s = &iStatus;
		User::RequestComplete(s, KErrNone);
		}
	else
		iShared->iUiStatus = &iStatus;
	iShared->iLock.Signal();
	}

void CPheDbgAcm::RunL()
	{
	iShared->iLock.Wait();
	iShared->iUiStatus = NULL;
	TInt state = iShared->iState;
	TBool stateNew = iShared->iStateNew;
	iShared->iStateNew = EFalse;
	HBufC8* data = iShared->iIn.Length() ? iShared->iIn.Alloc() : NULL;
	iShared->iIn.Zero();
	iShared->iLock.Signal();

	if (stateNew && state == 1 && !iConnected)
		{
		iConnected = ETrue;
		iServer.ChannelUp(*this);
		}
	if (data)
		{
		Received(*data);
		delete data;
		}
	if (stateNew && state < 0)
		{
		if (iConnected)
			{
			iConnected = EFalse;
			iServer.ChannelDown(*this, state);
			}
		else
			{
			TBuf<64> b;
			b.Format(_L("USB %S not available (%d)"), &iName, state);
			iServer.Log(b);
			}
		return;		// the worker has ended; Stop/Start the server to try again
		}
	Arm();
	}

TInt CPheDbgAcm::RunError(TInt /*aErr*/)
	{
	Arm();
	return KErrNone;
	}

void CPheDbgAcm::DoCancel()
	{
	iShared->iLock.Wait();
	if (iShared->iUiStatus)
		{
		iShared->iUiStatus = NULL;
		TRequestStatus* s = &iStatus;
		User::RequestComplete(s, KErrCancel);
		}
	iShared->iLock.Signal();
	}

// Queue the line for the worker; never blocks the UI thread.
void CPheDbgAcm::WriteL(const TDesC8& aLine)
	{
	TPheDbgAcmShared& sh = *iShared;
	sh.iLock.Wait();
	TInt need = sh.iOut.Length() + aLine.Length() + 1;
	TInt err = KErrNone;
	if (need > sh.iOut.MaxLength())
		err = sh.iOut.ReAlloc(need + 1024);
	if (err == KErrNone)
		{
		sh.iOut.Append(aLine);
		sh.iOut.Append('\n');
		if (sh.iWorkerWake)
			{
			iWorker.RequestComplete(sh.iWorkerWake, KErrNone);
			sh.iWorkerWake = NULL;
			}
		}
	sh.iLock.Signal();
	User::LeaveIfError(err);
	}

// ---------------------------------------------------------------- TCP channel (one client at a time)

CPheDbgTcp* CPheDbgTcp::NewL(CPheDbgServer& aServer, TInt aPort, TBool aLan)
	{
	CPheDbgTcp* self = new (ELeave) CPheDbgTcp(aServer);
	CleanupStack::PushL(self);
	self->ConstructL(aPort, aLan);
	CleanupStack::Pop(self);
	return self;
	}

void CPheDbgTcp::ConstructL(TInt aPort, TBool aLan)
	{
	iLine.CreateL(256);
	iPort = aPort;
	iLan = aLan;
	User::LeaveIfError(iSs.Connect());
	User::LeaveIfError(iTimer.CreateLocal());
	User::LeaveIfError(Listen());
	iName.Format(_L("TCP:%d"), aPort);
	Accept();
	}

TInt CPheDbgTcp::Listen()
	{
	if (iListenOpen)
		{
		iListen.Close();
		iListenOpen = EFalse;
		}
	TInt err = iListen.Open(iSs, KAfInet, KSockStream, KProtocolInetTcp);
	if (err != KErrNone)
		return err;
	iListenOpen = ETrue;
	TInetAddr addr(iLan ? KInetAddrAny : KInetAddrLoop, iPort);
	iListen.SetOpt(KSoReuseAddr, KSolInetIp, 1);
	err = iListen.Bind(addr);
	if (err == KErrNone)
		err = iListen.Listen(1);
	return err;
	}

// The Wi-Fi link dropped (or the access point changed): the listening socket is dead and every Accept fails at once.
// Close everything and try again in 2 s, for as long as it takes; the server stays up and resumes by itself.
void CPheDbgTcp::Relisten()
	{
	if (iSockOpen)
		{
		iSock.Close();
		iSockOpen = EFalse;
		}
	if (iListenOpen)
		{
		iListen.Close();
		iListenOpen = EFalse;
		}
	iConnected = EFalse;
	iWaiting = ETrue;
	iTimer.After(iStatus, 2000000);
	SetActive();
	}

CPheDbgTcp::~CPheDbgTcp()
	{
	Cancel();
	if (iSockOpen)
		iSock.Close();
	if (iListenOpen)
		iListen.Close();
	iTimer.Close();
	iSs.Close();
	}

void CPheDbgTcp::Accept()
	{
	if (iSockOpen)
		{
		iSock.Close();
		iSockOpen = EFalse;
		}
	iConnected = EFalse;
	if (!iListenOpen || iSock.Open(iSs) != KErrNone)
		{
		Relisten();
		return;
		}
	iSockOpen = ETrue;
	iAccepting = ETrue;
	iListen.Accept(iSock, iStatus);
	SetActive();
	}

void CPheDbgTcp::Issue()
	{
	iRead.Zero();
	iSock.RecvOneOrMore(iRead, 0, iStatus, iLen);
	SetActive();
	}

void CPheDbgTcp::RunL()
	{
	if (iWaiting)
		{
		iWaiting = EFalse;
		if (Listen() != KErrNone)
			{
			Relisten();						// no network yet: keep waiting
			return;
			}
		iFails = 0;
		iServer.Log(_L("TCP listening again"));
		Accept();
		return;
		}
	if (iAccepting)
		{
		iAccepting = EFalse;
		if (iStatus.Int() != KErrNone)
			{
			if (++iFails >= 3)
				{
				iServer.Log(_L("TCP listener lost (network change): re-listening"));
				Relisten();
				}
			else
				Accept();
			return;
			}
		iFails = 0;
		iConnected = ETrue;
		iLine.Zero();
		iServer.ChannelUp(*this);
		Issue();
		return;
		}
	if (iStatus.Int() != KErrNone)
		{
		iServer.ChannelDown(*this, iStatus.Int());	// client left: wait for the next one
		Accept();
		return;
		}
	Received(iRead);
	if (iConnected)
		Issue();
	else
		Accept();							// a write failed (the link went away): wait for the next host
	}

TInt CPheDbgTcp::RunError(TInt /*aErr*/)
	{
	Accept();
	return KErrNone;
	}

void CPheDbgTcp::DoCancel()
	{
	if (iWaiting)
		iTimer.Cancel();
	else if (iAccepting)
		iListen.CancelAccept();
	else
		iSock.CancelRecv();
	}

void CPheDbgTcp::WriteL(const TDesC8& aLine)
	{
	if (!iConnected)
		return;
	// timed: a host that vanished with the Wi-Fi must not freeze the app (the write would never complete)
	RTimer t;
	if (t.CreateLocal() != KErrNone)
		return;
	TBuf8<1> nl;
	nl.Append('\n');
	for (TInt part = 0; part < 2 && iConnected; part++)
		{
		TRequestStatus st, ts;
		if (part == 0)
			iSock.Write(aLine, st);
		else
			iSock.Write(nl, st);
		t.After(ts, 10000000);
		User::WaitForRequest(st, ts);
		if (st == KRequestPending)
			{
			iSock.CancelWrite();
			User::WaitForRequest(st);
			iConnected = EFalse;			// RunL re-accepts after this request
			iServer.Log(_L("TCP write timed out: host gone"));
			}
		else
			{
			t.Cancel();
			User::WaitForRequest(ts);
			if (st.Int() != KErrNone)
				iConnected = EFalse;
			}
		}
	t.Close();
	}

// ---------------------------------------------------------------- server

CPheDbgServer* CPheDbgServer::NewL(TPheDbgSettings& aSettings, MPheDbgUi& aUi, RFs& aFs)
	{
	CPheDbgServer* self = new (ELeave) CPheDbgServer(aSettings, aUi, aFs);
	CleanupStack::PushL(self);
	self->ConstructL();
	CleanupStack::Pop(self);
	return self;
	}

void CPheDbgServer::ConstructL()
	{
	iLog = new (ELeave) CDesCArrayFlat(16);
	iAllow = new (ELeave) CDesCArrayFlat(4);
	LoadAllowListL();
	UpdateStatus();
	}

CPheDbgServer::~CPheDbgServer()
	{
	Stop();
	delete iLog;
	delete iAllow;
	}

void CPheDbgServer::StartL()
	{
	if (iRunning)
		return;
	if (iSettings.iTransport & TPheDbgSettings::EUsb)
		{
		CPheDbgAcm* acm = NULL;
		TRAPD(err, acm = CPheDbgAcm::NewL(*this, iSettings.iAcmUnit));
		TBuf<64> b;
		if (err == KErrNone)
			{
			iChannels.AppendL(acm);		// says hello (ChannelUp) once its worker has the port open
			b.Format(_L("USB ACM::%d opening"), iSettings.iAcmUnit);
			}
		else
			b.Format(_L("USB ACM::%d not available (%d)"), iSettings.iAcmUnit, err);
		Log(b);
		}
	if (iSettings.iTransport & TPheDbgSettings::ETcp)
		{
		CPheDbgTcp* tcp = NULL;
		TRAPD(err, tcp = CPheDbgTcp::NewL(*this, iSettings.iTcpPort, iSettings.iTcpLan));
		if (err == KErrNone)
			{
			iChannels.AppendL(tcp);
			TBuf<64> b;
			TPtrC where(iSettings.iTcpLan ? _L("LAN") : _L("this phone only"));
			b.Format(_L("TCP listening on port %d (%S)"), iSettings.iTcpPort, &where);
			Log(b);
			}
		else
			{
			TBuf<64> b;
			b.Format(_L("TCP port %d not available (%d)"), iSettings.iTcpPort, err);
			Log(b);
			}
		}
	iRunning = iChannels.Count() > 0;
	Log(iRunning ? _L("server started") : _L("server not started: no transport available"));
	UpdateStatus();
	}

void CPheDbgServer::Stop()
	{
	iChannels.ResetAndDestroy();
	if (iRunning)
		{
		iRunning = EFalse;
		Log(_L("server stopped"));
		}
	UpdateStatus();
	}

void CPheDbgServer::UpdateStatus()
	{
	if (!iRunning)
		iStatus = _L("phedbg: OFF");
	else
		{
		iStatus = _L("phedbg: ON (");
		for (TInt i = 0; i < iChannels.Count(); i++)
			{
			if (i)
				iStatus.Append('/');
			iStatus.Append(iChannels[i]->Name().Left(8));
			}
		iStatus.Append(')');
		}
	iUi.StatusChanged();
	}

void CPheDbgServer::Log(const TDesC& aText)
	{
	TTime now;
	now.HomeTime();
	TBuf<160> line;
	TRAP_IGNORE(now.FormatL(line, _L("%J%:1%T%:2%S ")));
	line.Append(aText.Left(line.MaxLength() - line.Length()));
	if (iLog->Count() >= KPheDbgLogLines)
		iLog->Delete(0);
	TRAP_IGNORE(iLog->AppendL(line));
	iUi.StatusChanged();
	}

void CPheDbgServer::ChannelUp(CPheDbgChannel& aCh)
	{
	_LIT8(KHello, "@0 hello proto=1 agent=phedbg ver=%d.%d ops=info,fs_list,fs_read,fs_write,fs_delete,exec,ps,screenshot,log");
	TBuf8<176> hello;
	hello.Format(KHello, KPheDbgVersionMajor, KPheDbgVersionMinor);
	if (PHEDBG_CAP_SWEVENT)
		hello.Append(_L8(",key,touch"));	// input works only in a build that was granted SwEvent
	TRAP_IGNORE(aCh.WriteL(hello));
	TBuf<48> b;
	b.Format(_L("host connected on %S"), &aCh.Name());
	Log(b);
	}

void CPheDbgServer::ChannelDown(CPheDbgChannel& aCh, TInt aErr)
	{
	if (aCh.iWriteOpen)
		{
		aCh.iWriteFile.Close();
		aCh.iWriteOpen = EFalse;
		}
	TBuf<48> b;
	b.Format(_L("host left %S (%d)"), &aCh.Name(), aErr);
	Log(b);
	}

// ---- allow-list (one path per line in the private dir's allow.txt; the defaults are always allowed)

void CPheDbgServer::LoadAllowListL()
	{
	iAllow->Reset();
	TFileName name;
	iFs.PrivatePath(name);
	name.Insert(0, _L("c:"));
	name.Append(_L("allow.txt"));
	RFile f;
	if (f.Open(iFs, name, EFileRead | EFileShareReadersOnly) != KErrNone)
		return;
	CleanupClosePushL(f);
	TBuf8<1024> buf;
	f.Read(buf);
	TLex8 lex(buf);
	while (!lex.Eos())
		{
		TPtrC8 line = lex.NextToken();
		if (line.Length() && line.Length() < KMaxFileName)
			{
			TFileName p;
			p.Copy(line);
			iAllow->AppendL(p);
			}
		}
	CleanupStack::PopAndDestroy(&f);
	}

void CPheDbgServer::SaveAllowListL()
	{
	TFileName name;
	iFs.PrivatePath(name);
	name.Insert(0, _L("c:"));
	iFs.MkDirAll(name);
	name.Append(_L("allow.txt"));
	RFile f;
	User::LeaveIfError(f.Replace(iFs, name, EFileWrite));
	CleanupClosePushL(f);
	for (TInt i = 0; i < iAllow->Count(); i++)
		{
		TBuf8<KMaxFileName + 2> line;
		line.Copy((*iAllow)[i]);
		line.Append(_L8("\r\n"));
		f.Write(line);
		}
	CleanupStack::PopAndDestroy(&f);
	}

// A write or delete is allowed only inside c:\data\phedbg\, the app's own private dir, or a user-listed folder.
// Wildcards and ".." are refused, so a path cannot step out of its folder.
TBool CPheDbgServer::AllowedWrite(const TDesC& aPath) const
	{
	if (aPath.Find(_L("..")) != KErrNotFound || aPath.Locate('*') != KErrNotFound || aPath.Locate('?') != KErrNotFound)
		return EFalse;
	if (aPath.Length() < 4 || aPath[1] != ':' || aPath[2] != '\\')
		return EFalse;
	if (aPath.Left(KPheDbgPrivAllow().Length()).CompareF(KPheDbgPrivAllow) == 0)
		return ETrue;
	TFileName priv;
	const_cast<RFs&>(iFs).PrivatePath(priv);			// "\private\<uid>\"
	if (aPath.Mid(2).Left(priv.Length()).CompareF(priv) == 0)
		return ETrue;
	for (TInt i = 0; i < iAllow->Count(); i++)
		{
		TPtrC dir = (*iAllow)[i];
		if (dir.Length() >= 4 && dir[dir.Length() - 1] == '\\' && aPath.Left(dir.Length()).CompareF(dir) == 0)
			return ETrue;
		}
	return EFalse;
	}

// ---- replies

void CPheDbgServer::ReplyOkL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC8& aRest)
	{
	RBuf8 l;
	l.CreateL(aId.Length() + aRest.Length() + 8);
	CleanupClosePushL(l);
	l.Append('@');
	l.Append(aId);
	l.Append(_L8(" ok"));
	if (aRest.Length())
		{
		l.Append(' ');
		l.Append(aRest);
		}
	aCh.WriteL(l);
	CleanupStack::PopAndDestroy(&l);
	}

void CPheDbgServer::ReplyErrL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC8& aCode, const TDesC& aMsg)
	{
	RBuf8 l;
	l.CreateL(64);
	CleanupClosePushL(l);
	l.Append('@');
	l.Append(aId.Left(16));
	l.Append(_L8(" err code="));
	l.Append(aCode);
	if (aMsg.Length())
		{
		l.Append(_L8(" msg="));
		PheDbgProto::PercentEncodeL(aMsg, l);
		}
	aCh.WriteL(l);
	CleanupStack::PopAndDestroy(&l);
	}

void CPheDbgServer::SendDataL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC8& aData)
	{
	for (TInt pos = 0; pos < aData.Length(); pos += KPheDbgDataChunk)
		{
		RBuf8 l;
		l.CreateL(aId.Length() + 4 + (KPheDbgDataChunk + 2) / 3 * 4);
		CleanupClosePushL(l);
		l.Append('@');
		l.Append(aId);
		l.Append(_L8(" d "));
		PheDbgProto::Base64EncodeL(aData.Mid(pos, Min(KPheDbgDataChunk, aData.Length() - pos)), l);
		aCh.WriteL(l);
		CleanupStack::PopAndDestroy(&l);
		}
	}

static void LogReq(CPheDbgServer& aSrv, const TDesC8& aOp, const TDesC& aArg, const TDesC& aVerdict)
	{
	TBuf<150> b;
	TBuf<16> op;
	op.Copy(aOp.Left(16));
	b.Format(_L("%S %S: %S"), &op, &aVerdict, &aArg);
	aSrv.Log(b.Left(b.MaxLength()));
	}

// ---- dispatch

void CPheDbgServer::HandleLineL(CPheDbgChannel& aCh, const TDesC8& aLine)
	{
	TLex8 lex(aLine);
	TPtrC8 id = lex.NextToken();
	TPtrC8 op = lex.NextToken();
	if (!id.Length() || !op.Length() || id.Length() > 16)
		return;							// not a request (an empty line or noise on the port)

	// a payload line of a streaming fs_write: "<id> d <base64>"
	if (op == _L8("d"))
		{
		if (aCh.iWriteOpen && aCh.iWriteId > 0)
			OpWriteDataL(aCh, id, lex.Remainder());
		else
			ReplyErrL(aCh, id, _L8("badarg"), _L("no write in progress"));
		return;
		}

	iRequests++;
	TPtrC8 tok;
	TBuf8<16> code;
	code.Copy(iSettings.iCode);
	if (!iSettings.iCode.Length() || !PheDbgProto::Field(aLine, KTok, tok) || tok != code)
		{
		LogReq(*this, op, KNullDesC, _L("refused (wrong pairing code)"));
		ReplyErrL(aCh, id, _L8("auth"), KNullDesC);
		return;
		}

	RBuf path;
	CleanupClosePushL(path);
	TPtrC8 v;
	if (PheDbgProto::Field(aLine, KPath, v))
		PheDbgProto::PercentDecodeL(v, path);

	if (op == _L8("info"))
		OpInfoL(aCh, id);
	else if (op == _L8("fs_list"))
		{
		if (!iSettings.iOpRead) { ReplyErrL(aCh, id, _L8("unsupported"), _L("file read is off")); }
		else OpListL(aCh, id, path);
		}
	else if (op == _L8("fs_read"))
		{
		if (!iSettings.iOpRead) { ReplyErrL(aCh, id, _L8("unsupported"), _L("file read is off")); }
		else OpReadL(aCh, id, path);
		}
	else if (op == _L8("fs_write"))
		{
		TInt size = -1;
		if (PheDbgProto::Field(aLine, _L8("size"), v))
			TLex8(v).Val(size);
		if (!iSettings.iOpWrite) { ReplyErrL(aCh, id, _L8("unsupported"), _L("file write is off")); }
		else OpWriteL(aCh, id, path, size);
		}
	else if (op == _L8("fs_delete"))
		{
		if (!iSettings.iOpWrite) { ReplyErrL(aCh, id, _L8("unsupported"), _L("file write is off")); }
		else OpDeleteL(aCh, id, path);
		}
	else if (op == _L8("exec"))
		{
		RBuf args;
		CleanupClosePushL(args);
		if (PheDbgProto::Field(aLine, _L8("args"), v))
			PheDbgProto::PercentDecodeL(v, args);
		TBool wait = PheDbgProto::Field(aLine, _L8("wait"), v) && v == _L8("1");
		if (!iSettings.iOpExec) { ReplyErrL(aCh, id, _L8("unsupported"), _L("exec is off")); }
		else OpExecL(aCh, id, path, args, wait);
		CleanupStack::PopAndDestroy(&args);
		}
	else if (op == _L8("ps"))
		OpPsL(aCh, id);
	else if (op == _L8("screenshot"))
		{
		if (!iSettings.iOpShot) { ReplyErrL(aCh, id, _L8("unsupported"), _L("screenshot is off")); }
		else OpShotL(aCh, id);
		}
	else if (op == _L8("log"))
		{
		if (!iSettings.iOpLog) { ReplyErrL(aCh, id, _L8("unsupported"), _L("log is off")); }
		else OpLogL(aCh, id);
		}
	else if (op == _L8("key"))
		{
		if (!iSettings.iOpInput) { ReplyErrL(aCh, id, _L8("unsupported"), _L("input is off")); }
		else if (!PHEDBG_CAP_SWEVENT) { ReplyErrL(aCh, id, _L8("unsupported"), _L("SwEvent not granted")); }
		else
			{
			TPtrC8 sv;
			TInt scan = 0;
			if (PheDbgProto::Field(aLine, _L8("code"), sv) || PheDbgProto::Field(aLine, _L8("scan"), sv))
				TLex8(sv).Val(scan);	// code= (the protocol name, what PhoneLab sends); scan= kept as an alias
			if (scan <= 0) { ReplyErrL(aCh, id, _L8("badarg"), _L("code missing")); }
			else
				{
				TRawEvent ev;
				ev.Set(TRawEvent::EKeyDown, scan);
				UserSvr::AddEvent(ev);
				User::After(50000);
				ev.Set(TRawEvent::EKeyUp, scan);
				UserSvr::AddEvent(ev);
				ReplyOkL(aCh, id, KNullDesC8);
				LogReq(*this, _L8("key"), KNullDesC, _L("ok"));
				}
			}
		}
	else if (op == _L8("touch"))
		{
		if (!iSettings.iOpInput) { ReplyErrL(aCh, id, _L8("unsupported"), _L("input is off")); }
		else if (!PHEDBG_CAP_SWEVENT) { ReplyErrL(aCh, id, _L8("unsupported"), _L("SwEvent not granted")); }
		else
			{
			TPtrC8 sv;
			TInt x = -1, y = -1;
			if (PheDbgProto::Field(aLine, _L8("x"), sv))
				TLex8(sv).Val(x);
			if (PheDbgProto::Field(aLine, _L8("y"), sv))
				TLex8(sv).Val(y);
			if (x < 0 || y < 0) { ReplyErrL(aCh, id, _L8("badarg"), _L("x and y required")); }
			else
				{
				TRawEvent ev;
				ev.Set(TRawEvent::EButton1Down, x, y);
				UserSvr::AddEvent(ev);
				User::After(50000);
				ev.Set(TRawEvent::EButton1Up, x, y);
				UserSvr::AddEvent(ev);
				ReplyOkL(aCh, id, KNullDesC8);
				LogReq(*this, _L8("touch"), KNullDesC, _L("ok"));
				}
			}
		}
	else
		ReplyErrL(aCh, id, _L8("unsupported"), _L("unknown op"));
	CleanupStack::PopAndDestroy(&path);
	}

// ---- ops

void CPheDbgServer::OpInfoL(CPheDbgChannel& aCh, const TDesC8& aId)
	{
	TInt machine = 0, ramFree = 0;
	HAL::Get(HALData::EMachineUid, machine);
	HAL::Get(HALData::EMemoryRAMFree, ramFree);
	TDriveList drives;
	iFs.DriveList(drives);
	TBuf8<32> dl;
	for (TInt d = 0; d < KMaxDrives; d++)
		if (drives[d])
			dl.Append('a' + d);
	TBuf8<200> r;
	TPtrC8 capSw(PHEDBG_CAP_SWEVENT ? _L8("swevent,") : _L8(""));
	TPtrC8 capFiles(PHEDBG_CAP_ALLFILES ? _L8("allfiles") : _L8("user"));
	r.Format(_L8("agent=phedbg ver=%d.%d model=0x%08x ram_free=%d drives=%S caps=%S%S"),
	         KPheDbgVersionMajor, KPheDbgVersionMinor, machine, ramFree, &dl, &capSw, &capFiles);
	ReplyOkL(aCh, aId, r);
	LogReq(*this, _L8("info"), KNullDesC, _L("ok"));
	}

void CPheDbgServer::OpListL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath)
	{
	TFileName spec(aPath.Left(KMaxFileName - 1));
	if (!spec.Length() || spec[spec.Length() - 1] != '\\')
		spec.Append('\\');
	CDir* dir = NULL;
	TInt err = iFs.GetDir(spec, KEntryAttNormal | KEntryAttDir | KEntryAttHidden, ESortByName, dir);
	if (err != KErrNone)
		{
		LogReq(*this, _L8("fs_list"), aPath, _L("failed"));
		ReplyErrL(aCh, aId, err == KErrPermissionDenied ? _L8("denied") : (err == KErrPathNotFound || err == KErrNotFound) ? _L8("notfound") : _L8("io"), KNullDesC);
		return;
		}
	CleanupStack::PushL(dir);
	for (TInt i = 0; i < dir->Count(); i++)
		{
		const TEntry& e = (*dir)[i];
		RBuf8 l;
		l.CreateL(64);
		CleanupClosePushL(l);
		l.Append('@');
		l.Append(aId);
		l.Append(_L8(" e name="));
		PheDbgProto::PercentEncodeL(e.iName, l);
		l.AppendFormat(_L8(" size=%d dir=%d"), e.iSize, e.IsDir() ? 1 : 0);
		aCh.WriteL(l);
		CleanupStack::PopAndDestroy(&l);
		}
	TBuf8<24> r;
	r.Format(_L8("count=%d"), dir->Count());
	CleanupStack::PopAndDestroy(dir);
	ReplyOkL(aCh, aId, r);
	LogReq(*this, _L8("fs_list"), aPath, _L("ok"));
	}

void CPheDbgServer::OpReadL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath)
	{
	RFile f;
	TInt err = f.Open(iFs, aPath, EFileRead | EFileShareReadersOrWriters);
	if (err != KErrNone)
		{
		LogReq(*this, _L8("fs_read"), aPath, _L("failed"));
		ReplyErrL(aCh, aId, err == KErrPermissionDenied ? _L8("denied") : (err == KErrNotFound || err == KErrPathNotFound) ? _L8("notfound") : _L8("io"), KNullDesC);
		return;
		}
	CleanupClosePushL(f);
	TInt size = 0;
	f.Size(size);
	RBuf8 chunk;
	chunk.CreateL(KPheDbgDataChunk);
	CleanupClosePushL(chunk);
	TInt total = 0;
	FOREVER
		{
		chunk.Zero();
		if (f.Read(chunk) != KErrNone || chunk.Length() == 0)
			break;
		SendDataL(aCh, aId, chunk);
		total += chunk.Length();
		}
	CleanupStack::PopAndDestroy(2, &f);
	TBuf8<24> r;
	r.Format(_L8("size=%d"), total);
	ReplyOkL(aCh, aId, r);
	LogReq(*this, _L8("fs_read"), aPath, _L("ok"));
	}

void CPheDbgServer::OpWriteL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath, TInt aSize)
	{
	if (aCh.iWriteOpen)
		{
		ReplyErrL(aCh, aId, _L8("badarg"), _L("another write is in progress"));
		return;
		}
	if (aSize < 0)
		{
		ReplyErrL(aCh, aId, _L8("badarg"), _L("size missing"));
		return;
		}
	if (!AllowedWrite(aPath))
		{
		LogReq(*this, _L8("fs_write"), aPath, _L("denied (not in the allow-list)"));
		ReplyErrL(aCh, aId, _L8("denied"), _L("not in the write allow-list"));
		return;
		}
	if (iSettings.iConfirm)
		{
		TBuf<200> q;
		q.Format(_L("Allow the host to write %S?"), &aPath);
		if (!iUi.ConfirmL(q))
			{
			LogReq(*this, _L8("fs_write"), aPath, _L("rejected on the phone"));
			ReplyErrL(aCh, aId, _L8("denied"), _L("rejected on the phone"));
			return;
			}
		}
	TParsePtrC parse(aPath);
	iFs.MkDirAll(parse.DriveAndPath());
	TInt err = aCh.iWriteFile.Replace(iFs, aPath, EFileWrite);
	if (err != KErrNone)
		{
		LogReq(*this, _L8("fs_write"), aPath, _L("failed"));
		ReplyErrL(aCh, aId, err == KErrPermissionDenied ? _L8("denied") : _L8("io"), KNullDesC);
		return;
		}
	aCh.iWriteOpen = ETrue;
	TLex8(aId).Val(aCh.iWriteId);
	aCh.iWriteLeft = aSize;
	LogReq(*this, _L8("fs_write"), aPath, _L("started"));
	if (aSize == 0)
		OpWriteDataL(aCh, aId, KNullDesC8);
	}

void CPheDbgServer::OpWriteDataL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC8& aB64)
	{
	TUint id = 0;
	TLex8(aId).Val(id);
	if (id != aCh.iWriteId)
		{
		ReplyErrL(aCh, aId, _L8("badarg"), _L("data for another request"));
		return;
		}
	RBuf8 raw;
	raw.CreateL(aB64.Length() * 3 / 4 + 4);
	CleanupClosePushL(raw);
	TInt err = PheDbgProto::Base64Decode(aB64, raw);
	if (err == KErrNone && raw.Length() > aCh.iWriteLeft)
		err = KErrOverflow;
	if (err == KErrNone && raw.Length())
		err = aCh.iWriteFile.Write(raw);
	if (err == KErrNone)
		aCh.iWriteLeft -= raw.Length();
	CleanupStack::PopAndDestroy(&raw);
	if (err != KErrNone)
		{
		aCh.iWriteFile.Close();
		aCh.iWriteOpen = EFalse;
		aCh.iWriteId = 0;
		ReplyErrL(aCh, aId, _L8("io"), KNullDesC);
		return;
		}
	if (aCh.iWriteLeft == 0)
		{
		TInt size = 0;
		aCh.iWriteFile.Size(size);
		aCh.iWriteFile.Close();
		aCh.iWriteOpen = EFalse;
		aCh.iWriteId = 0;
		TBuf8<24> r;
		r.Format(_L8("size=%d"), size);
		ReplyOkL(aCh, aId, r);
		}
	}

void CPheDbgServer::OpDeleteL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath)
	{
	if (!AllowedWrite(aPath))
		{
		LogReq(*this, _L8("fs_delete"), aPath, _L("denied (not in the allow-list)"));
		ReplyErrL(aCh, aId, _L8("denied"), _L("not in the write allow-list"));
		return;
		}
	if (iSettings.iConfirm)
		{
		TBuf<200> q;
		q.Format(_L("Allow the host to delete %S?"), &aPath);
		if (!iUi.ConfirmL(q))
			{
			LogReq(*this, _L8("fs_delete"), aPath, _L("rejected on the phone"));
			ReplyErrL(aCh, aId, _L8("denied"), _L("rejected on the phone"));
			return;
			}
		}
	TInt err = iFs.Delete(aPath);
	LogReq(*this, _L8("fs_delete"), aPath, err == KErrNone ? _L("ok") : _L("failed"));
	if (err == KErrNone)
		ReplyOkL(aCh, aId, KNullDesC8);
	else
		ReplyErrL(aCh, aId, (err == KErrNotFound || err == KErrPathNotFound) ? _L8("notfound") : _L8("io"), KNullDesC);
	}

// exec starts one installed program (an .exe name or a path ending in .exe). It is the same as starting an app from
// the menu: the program runs with its own capabilities, never more. With confirmation on, the user approves each one.
void CPheDbgServer::OpExecL(CPheDbgChannel& aCh, const TDesC8& aId, const TDesC& aPath, const TDesC& aArgs, TBool aWait)
	{
	TParsePtrC parse(aPath);
	if (!aPath.Length() || parse.Ext().CompareF(_L(".exe")) != 0 || aPath.Locate('*') != KErrNotFound)
		{
		ReplyErrL(aCh, aId, _L8("badarg"), _L("path must name an .exe"));
		return;
		}
	if (iSettings.iConfirm)
		{
		TBuf<200> q;
		q.Format(_L("Allow the host to start %S?"), &aPath);
		if (!iUi.ConfirmL(q))
			{
			LogReq(*this, _L8("exec"), aPath, _L("rejected on the phone"));
			ReplyErrL(aCh, aId, _L8("denied"), _L("rejected on the phone"));
			return;
			}
		}
	RProcess p;
	TInt err = p.Create(aPath, aArgs);
	if (err != KErrNone)
		{
		LogReq(*this, _L8("exec"), aPath, _L("failed"));
		TBuf<32> m;
		m.Format(_L("start failed (%d)"), err);
		ReplyErrL(aCh, aId, err == KErrNotFound ? _L8("notfound") : err == KErrPermissionDenied ? _L8("denied") : _L8("io"), m);
		return;
		}
	CleanupClosePushL(p);
	TBuf8<64> r;
	if (aWait)
		{
		TRequestStatus st;
		p.Logon(st);
		p.Resume();
		User::WaitForRequest(st);
		r.Format(_L8("pid=%u exit_type=%d exit_reason=%d"), (TUint)p.Id(), p.ExitType(), p.ExitReason());
		}
	else
		{
		p.Resume();
		r.Format(_L8("pid=%u"), (TUint)p.Id());
		}
	CleanupStack::PopAndDestroy(&p);
	LogReq(*this, _L8("exec"), aPath, _L("started"));
	ReplyOkL(aCh, aId, r);
	}

void CPheDbgServer::OpPsL(CPheDbgChannel& aCh, const TDesC8& aId)
	{
	TFindProcess fp(_L("*"));
	TFullName name;
	TInt n = 0;
	while (fp.Next(name) == KErrNone)
		{
		RProcess p;
		TUint pid = 0;
		if (p.Open(fp) == KErrNone)
			{
			pid = (TUint)p.Id();
			p.Close();
			}
		RBuf8 l;
		l.CreateL(64);
		CleanupClosePushL(l);
		l.Append('@');
		l.Append(aId);
		l.Append(_L8(" e name="));
		PheDbgProto::PercentEncodeL(name, l);
		l.AppendFormat(_L8(" pid=%u"), pid);
		aCh.WriteL(l);
		CleanupStack::PopAndDestroy(&l);
		n++;
		}
	TBuf8<24> r;
	r.Format(_L8("count=%d"), n);
	ReplyOkL(aCh, aId, r);
	LogReq(*this, _L8("ps"), KNullDesC, _L("ok"));
	}

// The whole screen as a 24-bit bottom-up BMP.
void CPheDbgServer::OpShotL(CPheDbgChannel& aCh, const TDesC8& aId)
	{
	CWsScreenDevice* screen = CCoeEnv::Static()->ScreenDevice();
	TSize sz = screen->SizeInPixels();
	CFbsBitmap* bmp = new (ELeave) CFbsBitmap;
	CleanupStack::PushL(bmp);
	User::LeaveIfError(bmp->Create(sz, EColor16MU));
	TInt err = screen->CopyScreenToBitmap(bmp);
	if (err != KErrNone)
		{
		CleanupStack::PopAndDestroy(bmp);
		LogReq(*this, _L8("screenshot"), KNullDesC, _L("failed"));
		ReplyErrL(aCh, aId, err == KErrPermissionDenied ? _L8("denied") : _L8("io"), KNullDesC);
		return;
		}
	const TInt rowBytes = (sz.iWidth * 3 + 3) & ~3;
	const TInt imageBytes = rowBytes * sz.iHeight;
	RBuf8 out;
	out.CreateL(54 + imageBytes);
	CleanupClosePushL(out);
	out.SetLength(54);
	out.FillZ();
	TUint8* h = (TUint8*)out.Ptr();
	h[0] = 'B'; h[1] = 'M';
	TUint32 v;
	v = 54 + imageBytes; Mem::Copy(h + 2, &v, 4);
	v = 54; Mem::Copy(h + 10, &v, 4);
	v = 40; Mem::Copy(h + 14, &v, 4);
	v = sz.iWidth; Mem::Copy(h + 18, &v, 4);
	v = sz.iHeight; Mem::Copy(h + 22, &v, 4);
	h[26] = 1; h[28] = 24;
	v = imageBytes; Mem::Copy(h + 34, &v, 4);
	TBuf8<4> pad;
	pad.FillZ(rowBytes - sz.iWidth * 3);
	for (TInt y = sz.iHeight - 1; y >= 0; y--)
		{
		for (TInt x = 0; x < sz.iWidth; x++)
			{
			TRgb c;
			bmp->GetPixel(c, TPoint(x, y));
			out.Append((TUint8)c.Blue());
			out.Append((TUint8)c.Green());
			out.Append((TUint8)c.Red());
			}
		out.Append(pad);
		}
	SendDataL(aCh, aId, out);
	TBuf8<48> r;
	r.Format(_L8("fmt=bmp w=%d h=%d"), sz.iWidth, sz.iHeight);
	CleanupStack::PopAndDestroy(2, bmp);
	ReplyOkL(aCh, aId, r);
	LogReq(*this, _L8("screenshot"), KNullDesC, _L("ok"));
	}

void CPheDbgServer::OpLogL(CPheDbgChannel& aCh, const TDesC8& aId)
	{
	for (TInt i = 0; i < iLog->Count(); i++)
		{
		RBuf8 l;
		l.CreateL(64);
		CleanupClosePushL(l);
		l.Append('@');
		l.Append(aId);
		l.Append(_L8(" e text="));
		PheDbgProto::PercentEncodeL((*iLog)[i], l);
		aCh.WriteL(l);
		CleanupStack::PopAndDestroy(&l);
		}
	ReplyOkL(aCh, aId, KNullDesC8);
	}
