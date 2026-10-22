// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// phedbg_app.cpp -- the Avkon application shell for phedbg: settings list, options menu,
// settings persistence and pairing code generation.  The protocol and transport are in
// phedbg_server.cpp; this file provides only the UI and implements MPheDbgUi.

#include <aknapp.h>
#include <akndoc.h>
#include <aknappui.h>
#include <aknsettingitemlist.h>
#include <aknquerydialog.h>
#include <aknmessagequerydialog.h>
#include <aknlistquerydialog.h>
#include <akntitle.h>
#include <aknnotewrappers.h>
#include <eikstart.h>
#include <eikspane.h>
#include <avkon.hrh>
#include <avkon.rsg>
#include <e32math.h>

#include "phedbg.h"
#include "phedbg.hrh"
#include "phedbg_caps.h"
#include <phedbg.rsg>

// ---------------------------------------------------------------- settings file (c:\private\E7016B40\phedbg.ini)

static void SettingsPathL(RFs& aFs, TFileName& aPath)
	{
	aFs.PrivatePath(aPath);
	aPath.Insert(0, _L("c:"));
	aFs.MkDirAll(aPath);
	aPath.Append(_L("phedbg.ini"));
	}

static void LoadSettingsL(RFs& aFs, TPheDbgSettings& s)
	{
	s.SetDefaults();
	TFileName path;
	SettingsPathL(aFs, path);
	RFile f;
	if (f.Open(aFs, path, EFileRead | EFileShareReadersOnly) != KErrNone)
		return;
	CleanupClosePushL(f);
	TBuf8<512> buf;
	f.Read(buf);
	CleanupStack::PopAndDestroy(&f);
	TLex8 lex(buf);
	while (!lex.Eos())
		{
		lex.Mark();
		while (!lex.Eos() && lex.Peek() != '\n' && lex.Peek() != '\r')
			lex.Inc();
		TPtrC8 line(lex.MarkedToken());
		while (!lex.Eos() && (lex.Peek() == '\n' || lex.Peek() == '\r'))
			lex.Inc();
		TInt eq = line.Locate('=');
		if (eq <= 0)
			continue;
		TPtrC8 key(line.Left(eq));
		TPtrC8 val(line.Mid(eq + 1));
		TInt n = 0;
		TLex8 vl(val);
		if      (key == _L8("transport"))	{ vl.Val(n); s.iTransport = n; }
		else if (key == _L8("acm"))			{ vl.Val(n); s.iAcmUnit = n; }
		else if (key == _L8("port"))		{ vl.Val(n); s.iTcpPort = n; }
		else if (key == _L8("lan"))			{ vl.Val(n); s.iTcpLan = n; }
		else if (key == _L8("code"))		s.iCode.Copy(val.Left(s.iCode.MaxLength()));
		else if (key == _L8("read"))		{ vl.Val(n); s.iOpRead = n; }
		else if (key == _L8("write"))		{ vl.Val(n); s.iOpWrite = n; }
		else if (key == _L8("exec"))		{ vl.Val(n); s.iOpExec = n; }
		else if (key == _L8("shot"))		{ vl.Val(n); s.iOpShot = n; }
		else if (key == _L8("log"))			{ vl.Val(n); s.iOpLog = n; }
		else if (key == _L8("input"))		{ vl.Val(n); s.iOpInput = n; }
		else if (key == _L8("confirm"))		{ vl.Val(n); s.iConfirm = n; }
		else if (key == _L8("awake"))		{ vl.Val(n); s.iKeepAwake = n; }
		}
	s.iServer = 0;
	}

static void SaveSettingsL(RFs& aFs, const TPheDbgSettings& s)
	{
	TFileName path;
	SettingsPathL(aFs, path);
	RFile f;
	User::LeaveIfError(f.Replace(aFs, path, EFileWrite));
	CleanupClosePushL(f);
	TBuf8<256> b;
	b.Format(_L8("transport=%d\nacm=%d\nport=%d\nlan=%d\n"),
		s.iTransport, s.iAcmUnit, s.iTcpPort, s.iTcpLan);
	f.Write(b);
	TBuf8<32> code8;
	code8.Copy(s.iCode);
	b.Format(_L8("code=%S\n"), &code8);
	f.Write(b);
	b.Format(_L8("read=%d\nwrite=%d\nexec=%d\nshot=%d\nlog=%d\ninput=%d\nconfirm=%d\nawake=%d\n"),
		s.iOpRead, s.iOpWrite, s.iOpExec, s.iOpShot, s.iOpLog, s.iOpInput, s.iConfirm, s.iKeepAwake);
	f.Write(b);
	CleanupStack::PopAndDestroy(&f);
	}

// ---------------------------------------------------------------- pairing code

static void GenerateCode(TDes& aCode)
	{
	TTime now;
	now.HomeTime();
	TInt64 seed = now.Int64() ^ (TInt64)User::FastCounter();
	TInt n = Math::Rand(seed) % 1000000;
	aCode.Format(_L("%06d"), n);
	}

// ---------------------------------------------------------------- CPheDbgApplication

class CPheDbgApplication : public CAknApplication
	{
	TUid AppDllUid() const { return TUid::Uid(0xE7016B40); }
	CApaDocument* CreateDocumentL();
	};

// ---------------------------------------------------------------- CPheDbgDocument

class CPheDbgDocument : public CAknDocument
	{
public:
	static CPheDbgDocument* NewL(CEikApplication& aApp)
		{ return new (ELeave) CPheDbgDocument(aApp); }
	CEikAppUi* CreateAppUiL();
private:
	CPheDbgDocument(CEikApplication& aApp) : CAknDocument(aApp) {}
	};

// ---------------------------------------------------------------- settings list observer

class MPheDbgListObserver
	{
public:
	virtual void SettingChangedL(TInt aSettingId) = 0;
	};

// ---------------------------------------------------------------- CPheDbgList

class CPheDbgList : public CAknSettingItemList
	{
public:
	CPheDbgList(TPheDbgSettings& aSettings, MPheDbgListObserver& aObserver)
		: iSettings(aSettings), iObserver(aObserver) {}
	CAknSettingItem* CreateSettingItemL(TInt aId);
	void EditItemL(TInt aIndex, TBool aCalledFromMenu);
	void SizeChanged() { if (ListBox()) ListBox()->SetRect(Rect()); }
private:
	TPheDbgSettings& iSettings;
	MPheDbgListObserver& iObserver;
	};

CAknSettingItem* CPheDbgList::CreateSettingItemL(TInt aId)
	{
	switch (aId)
		{
		case EPheDbgSetServer:		return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iServer);
		case EPheDbgSetTransport:	return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iTransport);
		case EPheDbgSetAcmUnit:		return new (ELeave) CAknIntegerEdwinSettingItem(aId, iSettings.iAcmUnit);
		case EPheDbgSetTcpPort:		return new (ELeave) CAknIntegerEdwinSettingItem(aId, iSettings.iTcpPort);
		case EPheDbgSetTcpLan:		return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iTcpLan);
		case EPheDbgSetCode:		return new (ELeave) CAknTextSettingItem(aId, iSettings.iCode);
		case EPheDbgSetOpRead:		return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iOpRead);
		case EPheDbgSetOpWrite:		return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iOpWrite);
		case EPheDbgSetOpExec:		return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iOpExec);
		case EPheDbgSetOpShot:		return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iOpShot);
		case EPheDbgSetOpLog:		return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iOpLog);
		case EPheDbgSetOpInput:		return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iOpInput);
		case EPheDbgSetConfirm:		return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iConfirm);
		case EPheDbgSetKeepAwake:	return new (ELeave) CAknEnumeratedTextPopupSettingItem(aId, iSettings.iKeepAwake);
		default:					return NULL;
		}
	}

void CPheDbgList::EditItemL(TInt aIndex, TBool aCalledFromMenu)
	{
	TInt id = (*SettingItemArray())[aIndex]->Identifier();
	if (id == EPheDbgSetCode)
		return;
	if (id == EPheDbgSetOpInput && !RProcess().HasCapability(ECapabilitySwEvent))
		return;
	CAknSettingItemList::EditItemL(aIndex, aCalledFromMenu);
	StoreSettingsL();
	iObserver.SettingChangedL(id);
	}

// ---------------------------------------------------------------- CPheDbgAppUi

class CPheDbgAppUi : public CAknAppUi, public MPheDbgUi, public MPheDbgListObserver
	{
public:
	void ConstructL();
	~CPheDbgAppUi();
	void HandleCommandL(TInt aCommand);
	void DynInitMenuPaneL(TInt aResourceId, CEikMenuPane* aMenuPane);
	// MPheDbgUi
	void StatusChanged();
	TBool ConfirmL(const TDesC& aWhat);
	// MPheDbgListObserver
	void SettingChangedL(TInt aSettingId);
private:
	void StatusChangedL();
	void UpdateKeepAwake();
	void EnsureNetworkL();
	static TInt KeepAwakeTick(TAny* aPtr);
	TPheDbgSettings iSettings;
	CPheDbgServer* iServer;
	CPheDbgList* iList;
	CPeriodic* iKeepAwake;
	};

void CPheDbgAppUi::ConstructL()
	{
	BaseConstructL(EAknEnableSkin);
	LoadSettingsL(iEikonEnv->FsSession(), iSettings);
	if (iSettings.iCode.Length() == 0)
		{
		GenerateCode(iSettings.iCode);
		SaveSettingsL(iEikonEnv->FsSession(), iSettings);
		}
	iSettings.iServer = 0;
	iServer = CPheDbgServer::NewL(iSettings, *this, iEikonEnv->FsSession());
	iList = new (ELeave) CPheDbgList(iSettings, *this);
	iList->SetMopParent(this);
	iList->ConstructFromResourceL(R_PHEDBG_SETTINGS);
	iList->SetRect(ClientRect());
	iList->ActivateL();
	AddToStackL(iList);
	UpdateKeepAwake();
	StatusChanged();
	}

CPheDbgAppUi::~CPheDbgAppUi()
	{
	delete iKeepAwake;
	if (iList)
		{
		RemoveFromStack(iList);
		delete iList;
		}
	delete iServer;
	}

TInt CPheDbgAppUi::KeepAwakeTick(TAny*)
	{
	User::ResetInactivityTime();
	return KErrNone;
	}

void CPheDbgAppUi::UpdateKeepAwake()
	{
	if (iSettings.iKeepAwake && !iKeepAwake)
		{
		TRAPD(err, iKeepAwake = CPeriodic::NewL(CActive::EPriorityLow));
		if (err == KErrNone)
			iKeepAwake->Start(1, 5 * 1000000, TCallBack(KeepAwakeTick, NULL));
		}
	else if (!iSettings.iKeepAwake && iKeepAwake)
		{
		delete iKeepAwake;
		iKeepAwake = NULL;
		}
	}

void CPheDbgAppUi::EnsureNetworkL()
	{
	RSocketServ ss;
	User::LeaveIfError(ss.Connect());
	CleanupClosePushL(ss);
	RConnection conn;
	User::LeaveIfError(conn.Open(ss));
	CleanupClosePushL(conn);
	TInt err = conn.Start();
	if (err == KErrNone)
		{
		RSocket sock;
		if (sock.Open(ss, KAfInet, KSockDatagram, KProtocolInetUdp, conn) == KErrNone)
			{
			TInetAddr dest;
			dest.SetAddress(INET_ADDR(8, 8, 8, 8));
			dest.SetPort(53);
			TRequestStatus stat;
			sock.Connect(dest, stat);
			User::WaitForRequest(stat);
			if (stat.Int() == KErrNone)
				{
				TInetAddr local;
				sock.LocalName(local);
				TBuf<40> ip;
				local.Output(ip);
				TBuf<80> msg;
				msg.Format(_L("Connect at %S:%d"), &ip, iSettings.iTcpPort);
				iServer->Log(msg);
				CAknInformationNote* note = new (ELeave) CAknInformationNote(ETrue);
				note->ExecuteLD(msg);
				}
			sock.Close();
			}
		}
	else if (err == KErrCancel)
		{
		iServer->Log(_L("Wi-Fi not connected"));
		}
	CleanupStack::PopAndDestroy(2);
	}

void CPheDbgAppUi::HandleCommandL(TInt aCommand)
	{
	switch (aCommand)
		{
		case EPheDbgCmdStartStop:
			{
			if (iServer->Running())
				{
				iServer->Stop();
				iSettings.iServer = 0;
				}
			else
				{
				iServer->StartL();
				iSettings.iServer = 1;
				}
			iList->LoadSettingsL();
			iList->DrawNow();
			}
			break;

		case EPheDbgCmdChange:
			iList->EditItemL(iList->ListBox()->CurrentItemIndex(), ETrue);
			break;

		case EPheDbgCmdNewCode:
			{
			GenerateCode(iSettings.iCode);
			SaveSettingsL(iEikonEnv->FsSession(), iSettings);
			iList->LoadSettingsL();
			iList->DrawNow();
			TBuf<80> msg;
			msg.Format(_L("New code: %S.\nHosts using the old code are now refused."), &iSettings.iCode);
			CAknInformationNote* note = new (ELeave) CAknInformationNote(ETrue);
			note->ExecuteLD(msg);
			}
			break;

		case EPheDbgCmdAllowList:
			{
			RBuf text;
			text.CreateL(2048);
			CleanupClosePushL(text);
			TFileName priv;
			iEikonEnv->FsSession().PrivatePath(priv);
			priv.Insert(0, _L("c:"));
			text.Append(_L("c:\\data\\phedbg\\ (built-in)\n"));
			text.Append(priv);
			text.Append(_L(" (built-in)\n"));
			CDesCArray& al = iServer->AllowList();
			for (TInt i = 0; i < al.Count(); i++)
				{
				text.Append(al[i]);
				text.Append('\n');
				}
			CAknMessageQueryDialog* dlg = CAknMessageQueryDialog::NewL(text);
			dlg->PrepareLC(R_PHEDBG_MESSAGE_QUERY);
			dlg->QueryHeading()->SetTextL(_L("Write allow-list"));
			dlg->RunLD();
			CleanupStack::PopAndDestroy(&text);
			}
			break;

		case EPheDbgCmdAllowAdd:
			{
			TBuf<128> path;
			CAknTextQueryDialog* dlg = CAknTextQueryDialog::NewL(path, CAknQueryDialog::ENoTone);
			if (dlg->ExecuteLD(R_PHEDBG_PATH_QUERY))
				{
				TBool valid = path.Length() >= 4
					&& TChar(path[0]).IsAlpha()
					&& path[1] == ':'
					&& path[2] == '\\'
					&& path[path.Length() - 1] == '\\'
					&& path.Find(_L("..")) == KErrNotFound
					&& path.Locate('*') == KErrNotFound
					&& path.Locate('?') == KErrNotFound;
				if (valid)
					{
					iServer->AllowList().AppendL(path);
					iServer->SaveAllowListL();
					}
				else
					{
					CAknInformationNote* note = new (ELeave) CAknInformationNote(ETrue);
					note->ExecuteLD(_L("Invalid path. Must be X:\\...\\"));
					}
				}
			}
			break;

		case EPheDbgCmdAllowRemove:
			{
			CDesCArray& al = iServer->AllowList();
			if (al.Count() == 0)
				{
				CAknInformationNote* note = new (ELeave) CAknInformationNote(ETrue);
				note->ExecuteLD(_L("No user-added paths"));
				break;
				}
			TInt index = 0;
			CAknListQueryDialog* dlg = new (ELeave) CAknListQueryDialog(&index);
			dlg->PrepareLC(R_PHEDBG_LIST_QUERY);
			CDesCArrayFlat* items = new (ELeave) CDesCArrayFlat(4);
			CleanupStack::PushL(items);
			for (TInt i = 0; i < al.Count(); i++)
				items->AppendL(al[i]);
			CleanupStack::Pop(items);
			dlg->SetItemTextArray(items);
			dlg->SetOwnershipType(ELbmOwnsItemArray);
			if (dlg->RunLD())
				{
				al.Delete(index);
				iServer->SaveAllowListL();
				}
			}
			break;

		case EPheDbgCmdLog:
			{
			const CDesCArray& log = iServer->LogLines();
			RBuf text;
			text.CreateL(log.Count() * 200 + 64);
			CleanupClosePushL(text);
			if (log.Count() == 0)
				text.Append(_L("(no log entries)"));
			else
				for (TInt i = 0; i < log.Count(); i++)
					{
					text.Append(log[i]);
					text.Append('\n');
					}
			CAknMessageQueryDialog* mdlg = CAknMessageQueryDialog::NewL(text);
			mdlg->PrepareLC(R_PHEDBG_MESSAGE_QUERY);
			mdlg->QueryHeading()->SetTextL(_L("Session log"));
			mdlg->RunLD();
			CleanupStack::PopAndDestroy(&text);
			}
			break;

		case EAknSoftkeyExit:
		case EEikCmdExit:
			if (iServer)
				iServer->Stop();
			SaveSettingsL(iEikonEnv->FsSession(), iSettings);
			Exit();
			break;
		}
	}

void CPheDbgAppUi::DynInitMenuPaneL(TInt aResourceId, CEikMenuPane* aMenuPane)
	{
	if (aResourceId == R_PHEDBG_MENU_PANE)
		aMenuPane->SetItemTextL(EPheDbgCmdStartStop,
			iServer->Running() ? _L("Stop server") : _L("Start server"));
	}

void CPheDbgAppUi::StatusChanged()
	{
	TRAP_IGNORE(StatusChangedL());
	}

void CPheDbgAppUi::StatusChangedL()
	{
	CEikStatusPane* sp = StatusPane();
	if (!sp)
		return;
	CAknTitlePane* title = static_cast<CAknTitlePane*>(
		sp->ControlL(TUid::Uid(EEikStatusPaneUidTitle)));
	if (title)
		{
		if (iServer)
			title->SetTextL(iServer->StatusText());
		else
			title->SetTextL(_L("phedbg: OFF"));
		}
	}

TBool CPheDbgAppUi::ConfirmL(const TDesC& aWhat)
	{
	iEikonEnv->RootWin().SetOrdinalPosition(0);
	CDesCArrayFlat* items = new (ELeave) CDesCArrayFlat(3);
	CleanupStack::PushL(items);
	items->AppendL(_L("Allow"));
	items->AppendL(_L("Allow all (this session)"));
	items->AppendL(_L("Deny"));
	TInt index = 2;
	CAknListQueryDialog* dlg = new (ELeave) CAknListQueryDialog(&index);
	dlg->PrepareLC(R_PHEDBG_CONFIRM_QUERY);
	CAknPopupHeadingPane* heading = dlg->QueryHeading();
	if (heading)
		heading->SetTextL(aWhat);
	dlg->SetItemTextArray(items);
	dlg->SetOwnershipType(ELbmOwnsItemArray);
	TBool accepted = dlg->RunLD();
	CleanupStack::Pop(items);
	if (!accepted)
		return EFalse;
	if (index == 1)
		iSettings.iConfirm = 0;
	return index != 2;
	}

void CPheDbgAppUi::SettingChangedL(TInt aSettingId)
	{
	SaveSettingsL(iEikonEnv->FsSession(), iSettings);
	if (aSettingId == EPheDbgSetServer)
		{
		if (iSettings.iServer && !iServer->Running())
			{
			iServer->StartL();
			if (iServer->Running() && (iSettings.iTransport & TPheDbgSettings::ETcp) && iSettings.iTcpLan)
				EnsureNetworkL();
			}
		else if (!iSettings.iServer && iServer->Running())
			iServer->Stop();
		StatusChanged();
		}
	else if (aSettingId == EPheDbgSetKeepAwake)
		{
		UpdateKeepAwake();
		}
	else if (aSettingId == EPheDbgSetTransport || aSettingId == EPheDbgSetAcmUnit
			|| aSettingId == EPheDbgSetTcpPort || aSettingId == EPheDbgSetTcpLan)
		{
		if (iServer->Running())
			{
			CAknInformationNote* note = new (ELeave) CAknInformationNote(ETrue);
			note->ExecuteLD(_L("Restart the server to apply"));
			}
		}
	}

// ---------------------------------------------------------------- app framework glue

CApaDocument* CPheDbgApplication::CreateDocumentL()
	{
	return CPheDbgDocument::NewL(*this);
	}

CEikAppUi* CPheDbgDocument::CreateAppUiL()
	{
	return new (ELeave) CPheDbgAppUi;
	}

LOCAL_C CApaApplication* NewApplication()
	{
	return new CPheDbgApplication;
	}

GLDEF_C TInt E32Main()
	{
	return EikStart::RunApplication(NewApplication);
	}
