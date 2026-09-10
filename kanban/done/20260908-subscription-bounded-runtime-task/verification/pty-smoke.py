import os,sys,tempfile,pathlib,subprocess,pty,fcntl,termios,select,time,json
binary=sys.argv[1]
with tempfile.TemporaryDirectory(prefix='kander-runtime-pty-') as root:
    root=pathlib.Path(root)
    for state in ['backlog','todo','working','review','done','archived','trash']:
        (root/state).mkdir()
    task='20260908-runtime-smoke-task'
    group='20260908-runtime-smoke-group'
    path=root/'working'/task
    path.mkdir()
    window=os.environ.get('RUNTIME_SMOKE_WINDOW','foreground')
    (path/'spec.md').write_text(f'# 终端烟测\n\n- TYPE: Chore\n- SIZE: small\n- TASK_GROUP: {group}\n- SESSION: codex\n- WINDOW: {window}\n- OWNER: codex\n',encoding='utf-8')
    master,slave=pty.openpty()
    def terminal():
        os.setsid()
        fcntl.ioctl(0,termios.TIOCSCTTY,0)
    env=dict(os.environ,KANBAN_DIR=str(root))
    process=subprocess.Popen([binary,'subscribe','--refresh','.03','--heartbeat','.1',group,task],stdin=slave,stdout=slave,stderr=slave,env=env,preexec_fn=terminal)
    os.close(slave)
    raw=b''
    events=[]
    deadline=time.monotonic()+5
    try:
        while time.monotonic()<deadline:
            if select.select([master],[],[],.2)[0]:
                raw+=os.read(master,65536)
                events=[]
                for line in raw.decode('utf-8').splitlines():
                    if line.startswith('{'):
                        try: events.append(json.loads(line))
                        except json.JSONDecodeError: pass
                heartbeats=[x for x in events if x['event']=='heartbeat' and x.get('liveness',{}).get(task,{}).get('observed_at')]
                if heartbeats:break
        assert events and events[0]['event']=='snapshot',raw
        assert heartbeats,raw
        started=time.monotonic()
        os.write(master,b'\x03')
        code=process.wait(timeout=1)
        assert code==0,code
        live=heartbeats[-1]['liveness'][task]
        if window.startswith('herdr:'):assert live['status']=='alive',live
        print(json.dumps({'pty':True,'platform':sys.platform,'event_count':len(events),'snapshot':events[0]['event'],'heartbeat':heartbeats[-1]['event'],'channel':live['channel'],'status':live['status'],'runtime_state':live['runtime_state'],'observation_valid':live['observation_valid'],'ctrl_c_exit_code':code,'ctrl_c_join_ms':round((time.monotonic()-started)*1000,2),'real_herdr_agent_observation':window.startswith('herdr:')},ensure_ascii=False))
    finally:
        if process.poll() is None:process.kill();process.wait()
        os.close(master)
