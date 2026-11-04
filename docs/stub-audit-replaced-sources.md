# Sources left out of a build (stub audit, kind 5)

The registry's `drop=[...]` leaves source files of the real mmp out of a component's build and `extra=[...]` puts a file of
ours in their place (or `nosrc` replaces the whole component). Each line is a place where the image does not run the
release's code for that file; the replacement is ours, so none is a link stub, but each is a candidate to be audited
against "does it do what the real file does". Generated from the registry (`phe component list`, core at the time of
writing: `drop` 87 components, `nosrc` 4); see `stub-audit.md`.

| component | sources left out | replaced by |
|---|---|---|
| aifw | aieventhandler.cpp, aipluginfactory.cpp | aifw_board/aieventhandler.cpp, aifw_board/aipluginfactory.cpp |
| akncapserver | AknEikSgcs.cpp | akncapserver_trace/AknEikSgcs.cpp |
| alfappservercore | alfstreamerserver.cpp, alfstreamerbridge.cpp, alfappui.cpp, alfbridge.cpp, alfscreen.cpp | gen/alfstreamerserver_ph.cpp, gen/alfstreamerbridge_ph.cpp, gen/alfappui_ph.cpp, gen/alfbridge_ph.cpp, gen/alfscreen_ph.cpp, alf_fix/huitexturehandle_local.cpp |
| alfclient | (all: nosrc) | alf_null/alfclient_null.cpp |
| alfdecoderserverclient | (all: nosrc) | alf_null/alfdecoderserverclient_null.cpp |
| alfdecoderserverclient_real | alfdecoderserverclient.cpp | alf_fix/alfdecoderserverclient_ph.cpp |
| alfopenwfrs | openwfcwrapper.cpp, elementwrapper.cpp, screencaptureutil.cpp, displayrenderstage.cpp, displayrenderstagefactory.cpp | alfopenwfrs_board/openwfcwrapper.cpp, alfopenwfrs_board/elementwrapper.cpp, alfopenwfrs_board/screencaptureutil.cpp, gen/displayrenderstage_ph.cpp, alf_fix/displayrenderstagefactory_ph.cpp |
| alfrenderstage | alfrssendbuffer.cpp | alf_fix/alfrssendbuffer_ph.cpp |
| apparc | apacmdln.cpp | apparc_board/APACMDLN.CPP |
| arimp3decmmfcodec | arimp3decmmfcodec.cpp | mp3codec/arimp3decmmfcodec.cpp |
| audiocodec | audiocodec.cpp | audiocodec_gainfix/audiocodec.cpp |
| avkon | aknecs.cpp, AknAppUi.cpp, AknAdaptiveSearchGrid.cpp | stems/AknEcs.cpp, avkon_trace/AknAppUi.cpp, avkon_layout/AknAdaptiveSearchGrid.cpp |
| backend | ftable.cpp | backend_board/ftable.cpp |
| c32 | cc_utl.cpp, cs_man.cpp, cs_port.cpp, cs_ses.cpp, cs_svr.cpp, cs_time.cpp, cs_utl.cpp, cs_v02.cpp, cs_roles.cpp, cs_thread.cpp, cs_glob.cpp, cs_log.cpp | c32_null/c32_null.cpp |
| cconndlgplugin | ActiveSelectConnectionPlugin.cpp | conndlg_board/ActiveSelectConnectionPlugin.cpp |
| commsdat | CommsDatSessionImpl.cpp | commsdat_gcc/CommsDatSessionImpl.cpp |
| cone | COEPRIV.CPP | cone_diag/COEPRIV.CPP |
| connmon | ConnMonBearerLAN.cpp | connmon_board/ConnMonBearerLAN.cpp |
| cookiemanager | CookieArray.cpp, CookieClientData.cpp | cookiemanager_board/CookieArray.cpp, cookiemanager_board/CookieClientData.cpp |
| cshelp | csxhhtmltopiccontainer.cpp | cshelp_gcc/CSXHHtmlTopicContainer.cpp |
| csplugin | cspdevsound.cpp, cspaudiostreams.cpp | csplugin_board/cspdevsound.cpp, csplugin_board/cspaudiostreams.cpp |
| directgdiadapter | swdirectgdibitblt.cpp, swdirectgdidrawbmp.cpp, swdirectgdimain.cpp | directgdi_sw/swdirectgdibitblt.cpp, directgdi_sw/swdirectgdidrawbmp.cpp, directgdi_sw/swdirectgdimain.cpp, directgdi_sw/swdgdiflatten.cpp |
| disconnectdlg | ActiveDisconnectDlgPlugin.cpp | conndlg_board/ActiveDisconnectDlgPlugin.cpp |
| drmcrypto | cmlacrypto.cpp, omacrypto.cpp | stub/CmlaCryptoStub.cpp, src/OmaCrypto.cpp |
| e32strt | estart.cpp | estart_board/estart.cpp |
| eapol | eap_am_tools_symbian.cpp | eapol_gcc/eap_am_tools_symbian.cpp |
| eikcoctl | eikcba.cpp | eikcba_fix/eikcba.cpp |
| eiksrvui | eikkeysoundserver.cpp | eiksrvui_tone/eikkeysoundserver.cpp |
| eposlmdbmanlib | epos_hposlmdatabaseinfo.cpp | eposlmdbman_gcc/EPos_HPosLmDatabaseInfo.cpp |
| etext | TXTFMSTM.CPP | etext_gcc/TXTFMSTM.CPP |
| fbsrasterizer | svgtfbsrasterizer.cpp | svgtfbsrasterizer/factory.cpp, svgtfbsrasterizer/SvgtFbsRasterizer.cpp |
| frametable | frametable_stub.cpp | frametable/frametable.cpp |
| freetype | ftrast2.cpp | freetype_board/FTRAST2.CPP |
| gesturehelper | gesturehelperimpl.cpp, gesturerecogniser.cpp | gesturehelper_gcc/gesturehelperimpl.cpp, gesturehelper_gcc/gesturerecogniser.cpp |
| glxdataprovider | glxbinding.cpp | glx_gcc/glxbinding.cpp |
| gsdisplayplugin | PSMDisplayPlugin.cpp, GSDisplayPluginImplementationTable.cpp | gsdisplayplugin_gcc/GSDisplayPluginImplementationTable.cpp |
| hitchcock | HuiFxEffectParser.cpp, huienv.cpp, huidisplay.cpp, huicanvaswspainter.cpp, huirosterimpl.cpp, huicanvasvisual.cpp, huicanvastexturecache.cpp | alf_fix/HuiFxEffectParser_ph.cpp, gen/HuiEnv_ph.cpp, gen/HuiDisplay_ph.cpp, gen/huicanvaswspainter_ph.cpp, gen/HuiRosterImpl_ph.cpp, gen/HuiCanvasVisual_ph.cpp, gen/huicanvastexturecache_ph.cpp |
| hnmetadatamodel | hnxmlmodelprovider.cpp | stems/hnxmlmodelprovider.cpp |
| huirenderpluginvg | huivg10renderplugin.cpp, huivg10rendersurface.cpp, huivg10canvasrenderbuffer.cpp | alf_fix/huitexturehandle_local.cpp, gen/HuiVg10RenderPlugin_ph.cpp, gen/HuiVg10RenderSurface_ph.cpp, gen/huivg10canvasrenderbuffer_ph.cpp |
| hwrmstubplugins | PowerStatePlugin.cpp, LightPlugin.cpp, VibraPlugin.cpp | hwrm/PowerStatePlugin.cpp, hwrm/LightPlugin.cpp, hwrm/VibraPlugin.cpp |
| hwrmvibraclient | HWRMVibraImpl.cpp | hwrmvibra_board/HWRMVibraImpl.cpp |
| javascriptcore | snprintf.c | jsc_gcc/snprintf.c |
| keyrouter | keyrouter.cpp | keyrouter_board/keyrouter.cpp |
| libc | getenv.c | libc_findenv/getenv.c |
| libpthread | mutex.cpp, condvar.cpp, pthread_mutex_unlock.cpp, pthread_mutex_trylock.cpp, pthread_mutex_destroy.cpp | libpthread_board/mutex.cpp, libpthread_board/condvar.cpp, libpthread_board/pthread_mutex_unlock.cpp, libpthread_board/pthread_mutex_trylock.cpp, libpthread_board/pthread_mutex_destroy.cpp |
| libwfc | owfnotifications.cpp, wfcscene.c, wfccontext.c, owfthread.c, owfnativestreamstub.cpp | libwfc/owfnotifications.cpp, libwfc/owfnativestreamstub.cpp, libwfc/phtrace_wfc.cpp, libwfc/wfcscene.c, libwfc/wfccontext.c, libwfc/phgpucompose.cpp, libwfc/owfthread.c |
| licenseetsy | cmmmockmesshandler.cpp | licenseetsy_board/cmmmockmesshandler.cpp |
| lockapp | lockappappui.cpp | lockapp_fix/lockappappui.cpp |
| mcssathandler | mcssathandler.cpp | mcssathandler_null/mcssathandler_null.cpp |
| metadatautility | MetaDataParserRA.cpp | Src/MetaDataParserRADummy.cpp |
| mmfaudiotonecontroller | mmfaudiotonecontroller.cpp | tonecontroller_board/mmfaudiotonecontroller.cpp |
| mmrcserver | mmrcserverrulemanager.cpp | mmrc_mixing/mmrcserverrulemanager.cpp |
| mmwidgets | mmgridcontainer.cpp, mmlistboxcontainer.cpp | mm_fix/mmgridcontainer.cpp, mm_fix/mmlistboxcontainer.cpp |
| msgmedia | MmsConformance.cpp | msgmedia_gcc/MmsConformance.cpp |
| msgs | msvdbadapter.cpp | msgs_gcc/msvdbadapter.cpp |
| peninputgenericvkb | peninputgenericlafdatamgr.cpp | peninputgenericvkb_gcc/peninputgenericlafdatamgr.cpp |
| peninputimeplugingeneric | peninputimeplugingeneric.cpp | peninputimeplugingeneric_phim/peninputimeplugingeneric.cpp |
| peninputlayouteng | peninputimefinder.cpp | peninputlayouteng_keyfix/peninputimefinder.cpp |
| phoneengine | cpeparsermanufacturerhandler.cpp | phone_devmenu/cpeparsermanufacturerhandler.cpp |
| phoneparser | PhoneGsmParser.cpp | phone_devmenu/PhoneGsmParser.cpp |
| phoneuicontrol | cphonestatestartup.cpp, cphonestate.cpp | phoneuicontrol_board/cphonestatestartup.cpp, phone_devmenu/cphonestate.cpp |
| plan | securityinfo.cpp | oss.FCL.sf.mw.appinstall-group/securityinfo.cpp |
| ptikeymappings_itut_01 | ptikeymappings_itut_01.cpp | ptikeymap_board/PtiKeymappings_ITUT_01.cpp |
| recsisx | patchdata.cpp | sisxrecognizer/patchdata.cpp |
| scdv | Cdsb.cpp | scdv_cdsb/Cdsb.cpp |
| screensaver | screensaverappui.cpp | screensaver_fix/screensaverappui.cpp |
| sflibopenvgrefimpl | rieglos.cpp, rirasterizer.cpp, riimage.cpp, ripixelpipe.cpp, ripath.cpp | stems/riEGLOS.cpp, sfopenvg_board/riRasterizer.cpp, sfopenvg_board/riImage.cpp, sfopenvg_board/riPixelPipe.cpp, sfopenvg_board/riPhvg.cpp, sfopenvg_board/riPath.cpp |
| sflibopenvgrefimpl_alf | rieglos.cpp, rirasterizer.cpp, riimage.cpp, ripixelpipe.cpp, ripath.cpp, riminiegl.cpp, riapi.cpp | stems/riEGLOS.cpp, sfopenvg_board/riRasterizer.cpp, sfopenvg_board/riPixelPipe.cpp, sfopenvg_board/riPhvg.cpp, sfopenvg_board/riPath.cpp, gen/riImage_ph.cpp, gen/riMiniEGL_ph.cpp, alf_fix/ri_assert.cpp, gen/riApi_ph.cpp, alf_fix/ri_fastblit.cpp |
| sgresource | (all: nosrc) | shims/sgresource_null.cpp |
| sisregistryclient | securityinfo.cpp | oss.FCL.sf.mw.appinstall-group/securityinfo.cpp |
| smcm | SMSCLNT.CPP, SMUTHDR.CPP | smcm_fix/SMSCLNT.CPP, smcm_fix/SMUTHDR.CPP |
| smilmediarenderer | SmilSVGRenderer.cpp, SmilPlayerMediaFactory.cpp | smilui_gcc/SmilPlayerMediaFactory.cpp |
| spdctrl | speeddialprivate.cpp | speeddial_x32/speeddialprivate.cpp |
| startupadaptationstub | startupadaptationstub.cpp | sastub_board/StartupAdaptationStub.cpp |
| stmgesturefw | stateengine.cpp, statemachine.cpp | stmgesturefw_gcc/stateengine.cpp, stmgesturefw_gcc/statemachine.cpp |
| surfaceupdateclient | (all: nosrc) | shims/surfaceupdateclient_null.cpp |
| svgengine | svgmemorymanager.cpp | svg_fix/SVGMemoryManager.cpp |
| sysap | sysapdefaultkeyhandler.cpp, SysApLightsController.cpp, SysApAppUi.cpp | sysap_gcc/sysapdefaultkeyhandler.cpp, sysap_gcc/SysApLightsController.cpp, sysap_gcc/SysApAppUi.cpp |
| tactilevibraplugin | tactilevibraplayer.cpp | tactilevibra/tactilevibraplayer.cpp |
| tcpip6 | iface.cpp | tcpip6_gcc/iface.cpp |
| thumbnailserver | thumbnailserver.cpp | thumbnailserver_board/thumbnailserver.cpp |
| tonehwdevice | tonehwdevice.cpp | tonehwdevice_board/tonehwdevice.cpp, tonehwdevice_board/nokiatoneseq.cpp, tonehwdevice_board/ringbcconverter.cpp |
| tzserver | tzlocalizationdb.cpp | tz/tzlocalizationdb.cpp |
| vpbkcntmodel | cfilteredcontactview.cpp, cfilteredgroupview.cpp | vpbkcntmodel_board/CFilteredContactView.cpp, vpbkcntmodel_board/CFilteredGroupView.cpp |
| webcoreplatform | FormFillController.cpp, StaticObjectsContainer.cpp, HttpConnUtils.cpp | webcore_gcc/FormFillController.cpp, webcore_gcc/StaticObjectsContainer.cpp, webcore_gcc/HttpConnUtils.cpp |
| webkitutils | HistoryController.cpp, HistoryView.cpp | webkitutils_gcc/HistoryController.cpp, webkitutils_gcc/HistoryView.cpp |
| wfcinterop | contentupdateproxy.cpp, streammap.cpp | wfcinterop_board/contentupdateproxy.cpp, wfcinterop_board/streammap.cpp |
| ws32 | advancedpointerevent.cpp | ws32_board/advancedpointerevent.cpp |
| ws32_nga | RWINDOW.CPP | ws32_board/rwindow_nga.cpp |
| wserv | INIFILE.CPP | wserv_board/inifile_board.cpp |
| wserv_nga | INIFILE.CPP, CLIENT.CPP, WINBASE.CPP, POINTER.CPP, EVENT.CPP, DIRECT.CPP, SCREEN.CPP, GROUPWIN.CPP, wnredraw.cpp, redrawmsgwindow.cpp, cliwin.cpp | wserv_board/GroupWin_nga.cpp, wserv_board/Direct_nga.cpp, wserv_board/screen_nga.cpp, wserv_board/inifile_board.cpp, wserv_board/client_nga.cpp, wserv_board/winbase_nga.cpp, wserv_board/pointer_nga.cpp, wserv_board/event_nga.cpp, wserv_board/wnredraw_nga.cpp, wserv_board/redrawmsgwindow_nga.cpp, wserv_board/cliwin_nga.cpp |
| wsstdplugin_nga | openwfcwrapper.cpp | wsstdplugin_board/openwfcwrapper.cpp |
