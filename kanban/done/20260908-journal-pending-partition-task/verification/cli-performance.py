import json, os, pathlib, statistics, subprocess, tempfile, time
states=['backlog','todo','working','review','done','archived','trash']
task='20260908-benchmark-task'
results=[]
with tempfile.TemporaryDirectory(prefix='kander-journal-perf-') as tmp:
    for label, binary, partition in [('empty','/tmp/kander-journal-new',None),('before','/tmp/kander-journal-before',''),('partitioned','/tmp/kander-journal-new','committed')]:
        root=pathlib.Path(tmp)/label
        for state in states: (root/state).mkdir(parents=True)
        card=root/'backlog'/task
        card.mkdir()
        (card/'spec.md').write_text('# Benchmark\n- TYPE: Chore\n- SIZE: small\n')
        size=0
        if partition is not None:
            journal=root/'.kander'/'operations'/partition
            journal.mkdir(parents=True)
            for i in range(825):
                record={'schema':1,'operation_id':f'history-{i:04d}','phase':'committed','revisions':{task:i+1},'files':[{'path':f'backlog/{task}/spec.md','after':'x'*104000}]}
                data=json.dumps(record).encode()
                (journal/f'history-{i:04d}.json').write_bytes(data)
                size+=len(data)
        env=dict(os.environ,KANBAN_DIR=str(root))
        for args in [['show','--json',task],['list']]:
            samples=[]
            for _ in range(5):
                start=time.perf_counter()
                subprocess.run([binary,*args],env=env,cwd=tmp,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,check=True)
                samples.append(time.perf_counter()-start)
            results.append({'layout':label,'command':' '.join(args),'records':825 if partition is not None else 0,'bytes':size,'median_seconds':statistics.median(samples),'samples':samples})
        if label=='partitioned':
            p=subprocess.run([binary,'init'],env=env,cwd=tmp,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,check=True)
            print(p.stdout,p.stderr)
            print('post_init_committed_count',len(list((root/'.kander/operations/committed').glob('*.json'))))
    print(json.dumps(results,indent=2))
