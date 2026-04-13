package template

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Feature flags (bitmask) for tree-shaking
// ---------------------------------------------------------------------------

type jsFeature uint16

const (
	featCore         jsFeature = 1 << iota // always included
	featScope                              // always included (events/effects need it)
	featDebug                              // SPL.debug, debugRecord, getRenderStats
	featFocus                              // captureFocus, restoreFocus
	featBindings                           // patchBindings + helpers
	featEvents                             // patchEvents
	featModels                             // patchModels
	featAPI                                // patchAPI, apiParse, serializeForm
	featConditionals                       // patchConditionals
	featRefs                               // patchRefs (data-spl-ref element refs)
	featAll          = featCore | featScope | featDebug | featFocus | featBindings | featEvents | featModels | featAPI | featConditionals | featRefs
)

// ---------------------------------------------------------------------------
// Runtime modules — raw JS source for each segment
// ---------------------------------------------------------------------------

// moduleCore: SPL namespace, signals, subscribe, signalRef, signalName, interpolate, resolveTemplate
const moduleCore = `var SPL=window.__SPL__=window.__SPL__||{};
SPL.signals=SPL.signals||{};
SPL.handlers=SPL.handlers||{};
SPL.refs=SPL.refs||{};
SPL.registerHandler=function(name,fn){
  if(typeof name!=='string' || !name || typeof fn!=='function'){return;}
  SPL.handlers[name]=fn;
};
SPL.ensureSignal=function(name,initial){
  if(!SPL.signals[name]){SPL.signals[name]={value:initial,subscribers:[]};}
  return SPL.signals[name];
};
SPL.read=function(name){return SPL.ensureSignal(name,null).value;};
SPL.write=function(name,value){
  var s=SPL.ensureSignal(name,value);
  s.value=value;
  s.subscribers.forEach(function(fn){fn(value);});
};
SPL.subscribe=function(name,fn){
  var s=SPL.ensureSignal(name,null);
  s.subscribers.push(fn);
};
SPL.signalRef=function(name){
  var signal=SPL.ensureSignal(name,null);
  if(signal.ref){return signal.ref;}
  signal.ref={
    __splSignalName:name,
    valueOf:function(){return SPL.read(name);},
    toString:function(){var value=SPL.read(name);return value==null?'':String(value);}
  };
  if(typeof Symbol!=='undefined' && Symbol.toPrimitive){
    signal.ref[Symbol.toPrimitive]=function(hint){
      var value=SPL.read(name);
      if(value==null){
        return hint==='number'?0:'';
      }
      return value;
    };
  }
  return signal.ref;
};
SPL.signalName=function(nameOrRef){
  if(typeof nameOrRef==='string' && Object.prototype.hasOwnProperty.call(SPL.signals,nameOrRef)){
    return nameOrRef;
  }
  if(nameOrRef && typeof nameOrRef==='object' && typeof nameOrRef.__splSignalName==='string'){
    return nameOrRef.__splSignalName;
  }
  return '';
};
SPL.interpolate=function(source){
  return String(source||'').replace(/__SPL_SIGNAL__([A-Za-z0-9_.]+)__/g,function(_,path){
    var dot=path.indexOf('.');
    if(dot>=0){
      var value=SPL.readPath(path);
      if(value===true){return 'true';}
      if(value===false){return 'false';}
      if(value==null){return '';}
      if(typeof value==='object'){return JSON.stringify(value,null,2);}
      return String(value);
    }
    var value=SPL.read(path);
    if(value===true){return 'true';}
    if(value===false){return 'false';}
    if(value==null){return '';}
    if(typeof value==='object'){return JSON.stringify(value,null,2);}
    return String(value);
  });
};
SPL.resolveTemplate=function(source){
  return String(source||'').replace(/\{\{\s*([A-Za-z0-9_]+)\s*\}\}/g,function(_,name){
    var value=SPL.read(name);
    if(value==null){return '';}
    if(typeof value==='object'){return JSON.stringify(value);}
    return String(value);
  });
};
SPL.readPath=function(path){
  var dot=path.indexOf('.');
  if(dot<0){return SPL.read(path);}
  var root=path.slice(0,dot);
  var rest=path.slice(dot+1);
  var obj=SPL.read(root);
  if(obj==null){return undefined;}
  var parts=rest.split('.');
  for(var i=0;i<parts.length;i++){
    if(obj==null){return undefined;}
    obj=obj[parts[i]];
  }
  return obj;
};
SPL.writePath=function(path,value){
  var dot=path.indexOf('.');
  if(dot<0){SPL.write(path,value);return;}
  var root=path.slice(0,dot);
  var rest=path.slice(dot+1);
  var obj=SPL.read(root);
  if(obj==null){obj={};}
  if(typeof obj!=='object'){obj={};}
  var clone=JSON.parse(JSON.stringify(obj));
  var parts=rest.split('.');
  var cur=clone;
  for(var i=0;i<parts.length-1;i++){
    if(cur[parts[i]]==null || typeof cur[parts[i]]!=='object'){cur[parts[i]]={};}
    cur=cur[parts[i]];
  }
  cur[parts[parts.length-1]]=value;
  SPL.write(root,clone);
};`

// moduleScope: getScope proxy, expression evaluation, extractDeps
const moduleScope = `SPL.reservedNames={event:true,element:true,signal:true,toggle:true,setSignal:true,ref:true,select:true,selectAll:true,SPL:true,Math:true,Number:true,String:true,Boolean:true,Date:true,JSON:true,console:true,document:true,window:true,undefined:true,NaN:true,Infinity:true};
SPL.getScope=function(event,element,useRefs){
  var helpers={
    event:event,
    element:element,
    SPL:SPL,
    document:document,
    window:window,
    signal:function(name){return SPL.read(String(name));},
    toggle:function(nameOrRef){
      var name=SPL.signalName(nameOrRef);
      if(!name){return !Boolean(nameOrRef);}
      var next=!Boolean(SPL.read(name));
      SPL.write(name,next);
      return next;
    },
    setSignal:function(nameOrRef,value){
      var name=SPL.signalName(nameOrRef);
      if(!name){return value;}
      if(typeof value==='function'){
        var prev=SPL.read(name);
        value=value(prev);
      }
      SPL.write(name,value);
      return value;
    },
    ref:function(name){return SPL.refs[name]||null;},
    select:function(sel){return document.querySelector(sel);},
    selectAll:function(sel){return Array.from(document.querySelectorAll(sel));},
    Math:Math,
    Number:Number,
    String:String,
    Boolean:Boolean,
    Date:Date,
    JSON:JSON,
    console:console
  };
  return new Proxy(helpers,{
    has:function(target,key){
      if(typeof key==='symbol'){return key in target;}
      if(Object.prototype.hasOwnProperty.call(target,key)){return true;}
      if(Object.prototype.hasOwnProperty.call(SPL.handlers,key)){return true;}
      if(typeof globalThis!=='undefined' && key in globalThis){return false;}
      return Object.prototype.hasOwnProperty.call(SPL.signals,String(key));
    },
    get:function(target,key){
      if(typeof key==='symbol'){return target[key];}
      if(Object.prototype.hasOwnProperty.call(target,key)){return target[key];}
      if(Object.prototype.hasOwnProperty.call(SPL.handlers,key)){return SPL.handlers[key];}
      if(typeof globalThis!=='undefined' && key in globalThis){return globalThis[key];}
      if(useRefs){return SPL.signalRef(String(key));}
      return SPL.read(String(key));
    },
    set:function(target,key,value){
      if(typeof key==='symbol'){target[key]=value;return true;}
      if(Object.prototype.hasOwnProperty.call(target,key)){target[key]=value;return true;}
      SPL.write(String(key), value);
      return true;
    }
  });
};
SPL.statementCache=SPL.statementCache||{};
SPL.expressionCache=SPL.expressionCache||{};
SPL._cacheCount=SPL._cacheCount||{s:0,e:0};
SPL._evictCache=function(cache,countKey,max){
  if(SPL._cacheCount[countKey]<=max){return;}
  var keys=Object.keys(cache);
  var toRemove=keys.length>>2;
  for(var i=0;i<toRemove;i++){delete cache[keys[i]];}
  SPL._cacheCount[countKey]-=toRemove;
};
SPL.callHandler=function(result,event,element){
  if(typeof result!=='function'){return result;}
  return result.call(element,event,element,SPL.getScope(event,element,false));
};
SPL.runStatement=function(expr,event,element){
  var fn=SPL.statementCache[expr];
  if(!fn){
    try{fn=new Function('scope','event','element','with(scope){ '+expr+'; return undefined; }');}
    catch(e){if(typeof console!=='undefined'){console.error('[spl:stmt]',e);}return undefined;}
    SPL._evictCache(SPL.statementCache,'s',1000);
    SPL.statementCache[expr]=fn;
    SPL._cacheCount.s++;
  }
  return fn(SPL.getScope(event,element,true),event,element);
};
SPL.evalExpression=function(expr,event,element,useRefs){
  var fn=SPL.expressionCache[expr];
  if(!fn){
    fn=new Function('scope','event','element','with(scope){ return ('+expr+'); }');
    SPL._evictCache(SPL.expressionCache,'e',1000);
    SPL.expressionCache[expr]=fn;
    SPL._cacheCount.e++;
  }
  return fn(SPL.getScope(event,element,!!useRefs),event,element);
};
SPL.executeEvent=function(expr,event,element){
  try {
    return SPL.callHandler(SPL.evalExpression(expr,event,element,true),event,element);
  } catch (evalErr) {
    try {
      return SPL.callHandler(SPL.runStatement(expr,event,element),event,element);
    } catch (stmtErr) {
      if(typeof console!=='undefined' && console.error){
        console.error('[spl:event]', {expr:expr, evalError:evalErr, statementError:stmtErr});
      }
      throw stmtErr;
    }
  }
};
SPL.extractDeps=function(expr){
  var deps=[];var seen={};var inSingle=false;var inDouble=false;var prev='';
  for(var i=0;i<expr.length;i++){
    var ch=expr[i];
    if(ch==='"' && !inSingle && prev!=='\\'){inDouble=!inDouble;prev=ch;continue;}
    if(ch==="'" && !inDouble && prev!=='\\'){inSingle=!inSingle;prev=ch;continue;}
    if(inSingle||inDouble){prev=ch;continue;}
    if((ch>='A'&&ch<='Z')||(ch>='a'&&ch<='z')||ch==='_'){
      var start=i;
      i++;
      for(;i<expr.length;i++){
        var c=expr[i];
        if(!((c>='A'&&c<='Z')||(c>='a'&&c<='z')||(c>='0'&&c<='9')||c==='_')){break;}
      }
      var name=expr.slice(start,i);
      var before=start>0?expr[start-1]:'';
      if(before==='.'||SPL.reservedNames[name]){i--;prev=ch;continue;}
      if(!seen[name] && Object.prototype.hasOwnProperty.call(SPL.signals,name)){
        seen[name]=true;deps.push(name);
      }
      i--;prev=ch;continue;
    }
    prev=ch;
  }
  return deps;
};`

// moduleDebug: debug recording and render stats
const moduleDebug = `SPL.debug=SPL.debug||{enabled:true,totalRenders:0,views:{},effects:{},signals:{}};
SPL.debugRecord=function(kind,key){
  if(!SPL.debug || !SPL.debug.enabled){return;}
  SPL.debug.totalRenders=(SPL.debug.totalRenders||0)+1;
  if(kind==='view'){
    SPL.debug.views[key]=(SPL.debug.views[key]||0)+1;
  } else if(kind==='effect'){
    SPL.debug.effects[key]=(SPL.debug.effects[key]||0)+1;
  }
  if(typeof console!=='undefined' && console.debug){
    console.debug('[spl:render]', {kind:kind, key:key, total:SPL.debug.totalRenders, views:SPL.debug.views, effects:SPL.debug.effects});
  }
};
SPL.getRenderStats=function(){
  return {
    totalRenders:SPL.debug.totalRenders||0,
    views:Object.assign({}, SPL.debug.views||{}),
    effects:Object.assign({}, SPL.debug.effects||{}),
    signals:Object.keys(SPL.signals||{}).reduce(function(acc,key){acc[key]=SPL.read(key);return acc;}, {})
  };
};`

// moduleDebugStub: no-op stubs when debug is disabled
const moduleDebugStub = `SPL.debugRecord=function(){};SPL.getRenderStats=function(){return {};};`

// moduleFocus: focus capture/restore for re-renders
const moduleFocus = `SPL.captureFocus=function(root){
  var active=document.activeElement;
  if(!active || !root || active===document.body){return null;}
  if(active!==root && !(root.contains && root.contains(active))){return null;}
  var selector='';
  if(active.id){
    selector='#'+active.id;
  } else if(active.getAttribute){
    if(active.hasAttribute('data-spl-model')){
      selector='[data-spl-model="'+active.getAttribute('data-spl-model')+'"]';
    } else if(active.hasAttribute('data-spl-bind-value')){
      selector='[data-spl-bind-value="'+active.getAttribute('data-spl-bind-value')+'"]';
    } else if(active.name){
      selector='[name="'+active.name+'"]';
    }
  }
  if(!selector){return null;}
  return {
    selector:selector,
    start:typeof active.selectionStart==='number'?active.selectionStart:null,
    end:typeof active.selectionEnd==='number'?active.selectionEnd:null,
    checked:typeof active.checked==='boolean'?active.checked:null
  };
};
SPL.restoreFocus=function(root,snapshot){
  if(!root || !snapshot || !snapshot.selector || !root.querySelector){return;}
  var next=root.querySelector(snapshot.selector);
  if(!next || typeof next.focus!=='function'){return;}
  next.focus();
  if(snapshot.checked!==null && 'checked' in next){
    next.checked=snapshot.checked;
  }
  if(snapshot.start!==null && typeof next.setSelectionRange==='function'){
    var end=snapshot.end===null?snapshot.start:snapshot.end;
    next.setSelectionRange(snapshot.start,end);
  }
};`

// moduleFocusStub: no-op stubs when focus is not needed
const moduleFocusStub = `SPL.captureFocus=function(){return null;};SPL.restoreFocus=function(){};`

// moduleBindings: applyBinding, bindingEvent, readBindingValue, patchBindings
const moduleBindings = `SPL.applyBinding=function(el,prop,value){
  if(value!=null && typeof value==='object'){value=JSON.stringify(value,null,2);}
  if(prop==='html'){el.innerHTML=value==null?'':String(value);return;}
  if(prop in el){el[prop]=value==null?'':value;return;}
  el.setAttribute(prop,value==null?'':String(value));
};
SPL.bindingEvent=function(el,prop){
  if(prop==='checked' || prop==='selectedIndex' || el.tagName==='SELECT'){return 'change';}
  return 'input';
};
SPL.readBindingValue=function(el,prop){
  if(prop in el){return el[prop];}
  return el.getAttribute(prop);
};
SPL.patchBindings=function(root){
  var nodes=(root.matches && (root.matches('[data-spl-bind]') || Array.from(root.attributes||[]).some(function(attr){return attr.name.indexOf('data-spl-bind-')===0;})))?[root]:[];
  nodes=nodes.concat(Array.from(root.querySelectorAll ? root.querySelectorAll('[data-spl-bind], [data-spl-bind-value], [data-spl-bind-checked], [data-spl-bind-textContent], [data-spl-bind-innerHTML]') : []));
  nodes.forEach(function(el){
    var attrs=Array.from(el.attributes||[]);
    attrs.forEach(function(attr){
      if(attr.name==='data-spl-bind'){
        if(el.__splLegacyBound){return;}
        el.__splLegacyBound=true;
        var signalName=attr.value;
        var prop=el.getAttribute('data-spl-attr')||'textContent';
        var update=function(value){SPL.applyBinding(el,prop,value);};
        update(SPL.read(signalName));
        SPL.subscribe(signalName,update);
        if((prop==='value'||prop==='checked') && /^[A-Za-z_][A-Za-z0-9_]*$/.test(signalName)){
          el.addEventListener(SPL.bindingEvent(el,prop),function(){SPL.write(signalName,SPL.readBindingValue(el,prop));});
        }
        return;
      }
      if(attr.name.indexOf('data-spl-bind-')!==0){return;}
      var propName=attr.name.slice('data-spl-bind-'.length);
      var expr=attr.value;
      var bindKey='__splBind_'+propName+'_'+expr;
      if(el[bindKey]){return;}
      el[bindKey]=true;
      var updateExpr=function(){SPL.applyBinding(el,propName,SPL.evalExpression(expr,null,el,false));};
      updateExpr();
      SPL.extractDeps(expr).forEach(function(dep){SPL.subscribe(dep,updateExpr);});
      if(/^[A-Za-z_][A-Za-z0-9_]*$/.test(expr)){
        el.addEventListener(SPL.bindingEvent(el,propName),function(){SPL.write(expr,SPL.readBindingValue(el,propName));});
      }
    });
  });
};`

// moduleEvents: patchEvents
const moduleEvents = `SPL.patchEvents=function(root){
  var nodes=(root.matches && Array.from(root.attributes||[]).some(function(attr){return attr.name.indexOf('data-spl-on-')===0;}))?[root]:[];
  nodes=nodes.concat(Array.from(root.querySelectorAll ? root.querySelectorAll('*') : []));
  nodes.forEach(function(el){
    Array.from(el.attributes||[]).forEach(function(attr){
      if(attr.name.indexOf('data-spl-on-')!==0 || attr.name.indexOf('-mods')===attr.name.length-5){return;}
      var eventName=attr.name.slice('data-spl-on-'.length);
      var expr=attr.value;
      var key='__splEvent_'+eventName+'_'+expr;
      if(el[key]){return;}
      el[key]=true;
      var mods=(el.getAttribute('data-spl-on-'+eventName+'-mods')||'').split(',').map(function(v){return v.trim();}).filter(Boolean);
      var options={};
      if(mods.indexOf('capture')>=0){options.capture=true;}
      if(mods.indexOf('once')>=0){options.once=true;}
      if(mods.indexOf('passive')>=0){options.passive=true;}
      el.addEventListener(eventName,function(event){
        if(mods.indexOf('prevent')>=0 && event && typeof event.preventDefault==='function'){event.preventDefault();}
        if(mods.indexOf('stop')>=0 && event && typeof event.stopPropagation==='function'){event.stopPropagation();}
        SPL.executeEvent(expr,event,el);
      },options);
    });
  });
};`

// moduleModels: patchModels (supports dot-path binding e.g. data-spl-model="form.name")
const moduleModels = `SPL.patchModels=function(root){
  var nodes=(root.matches && root.matches('[data-spl-model]'))?[root]:[];
  nodes=nodes.concat(Array.from(root.querySelectorAll ? root.querySelectorAll('[data-spl-model]') : []));
  nodes.forEach(function(el){
    if(el.__splModelBound){return;}
    el.__splModelBound=true;
    var path=el.getAttribute('data-spl-model');
    var dot=path.indexOf('.');
    var signalName=dot<0?path:path.slice(0,dot);
    var isCheckOrRadio=(el.type==='checkbox'||el.type==='radio');
    var prop=isCheckOrRadio?'checked':'value';
    var update=function(){
      var value=dot<0?SPL.read(path):SPL.readPath(path);
      if(prop==='checked'){el.checked=Boolean(value);}else{el.value=value==null?'':String(value);}
    };
    update();
    var eventName=isCheckOrRadio?'change':'input';
    el.addEventListener(eventName,function(){
      var val=prop==='checked'?Boolean(el.checked):el.value;
      if(dot<0){SPL.write(path,val);}else{SPL.writePath(path,val);}
    });
    SPL.subscribe(signalName,update);
  });
};`

// moduleAPI: apiParse, serializeForm, patchAPI
const moduleAPI = `SPL.apiParse=function(res, mode){
  if(mode==='json'){return res.json();}
  if(mode==='text' || mode==='html'){return res.text();}
  var ct=(res.headers.get('content-type')||'').toLowerCase();
  if(ct.indexOf('application/json')>=0){return res.json();}
  return res.text();
};
SPL.serializeForm=function(form){
  var payload={};
  if(!form){return payload;}
  Array.from(form.elements||[]).forEach(function(field){
    if(!field.name || field.disabled){return;}
    if((field.type==='checkbox' || field.type==='radio')){
      payload[field.name]=Boolean(field.checked);
      return;
    }
    payload[field.name]=field.value;
  });
  return payload;
};
SPL.patchAPI=function(root){
  var nodes=(root.matches && root.matches('[data-spl-api-url]'))?[root]:[];
  nodes=nodes.concat(Array.from(root.querySelectorAll ? root.querySelectorAll('[data-spl-api-url]') : []));
  nodes.forEach(function(el){
    if(el.__splApiBound){return;}
    el.__splApiBound=true;
    var run=function(){
      var method=(el.getAttribute('data-spl-api-method')||'GET').toUpperCase();
      var url=SPL.resolveTemplate(el.getAttribute('data-spl-api-url')||'');
      var parseMode=(el.getAttribute('data-spl-api-parse')||'auto').toLowerCase();
      var target=el.getAttribute('data-spl-api-target')||'';
      var bodyTemplate=el.getAttribute('data-spl-api-body')||'';
      var resetSignals=(el.getAttribute('data-spl-api-reset')||'').split(',').map(function(v){return v.trim();}).filter(Boolean);
      var headers={};
      var body=null;
      if(bodyTemplate!==''){
        body=SPL.resolveTemplate(bodyTemplate);
        headers['Content-Type']=el.getAttribute('data-spl-api-content-type')||'application/json';
      } else {
        var form = null;
        if(el.tagName==='FORM'){
          form = el;
        } else if(el.getAttribute('data-spl-api-form')==='closest' && typeof el.closest==='function') {
          form = el.closest('form');
        }
        if(form){
          headers['Content-Type']=el.getAttribute('data-spl-api-content-type')||'application/json';
          body=JSON.stringify(SPL.serializeForm(form));
        }
      }
      return fetch(url,{method:method,headers:headers,body:body}).then(function(res){
        return SPL.apiParse(res,parseMode).then(function(payload){
          if(target){SPL.write(target,payload);}
          resetSignals.forEach(function(name){SPL.write(name,'');});
          return payload;
        });
      }).catch(function(err){
        if(target){SPL.write(target,'API error: '+err.message);}
      });
    };
    var eventName=(el.getAttribute('data-spl-api-event')||'click').toLowerCase();
    if(eventName==='load'){
      if(!el.__splApiLoaded){
        el.__splApiLoaded=true;
        run();
      }
      return;
    }
    el.addEventListener(eventName,function(ev){
      if(ev && typeof ev.preventDefault==='function' && (eventName==='submit' || el.tagName==='FORM' || (el.type==='submit' && typeof el.closest==='function' && el.closest('form')))){ev.preventDefault();}
      run();
    });
  });
};`

// moduleConditionals: patchConditionals
const moduleConditionals = `SPL.patchConditionals=function(root){
  var ifNodes=(root.matches && root.matches('[data-spl-if]'))?[root]:[];
  ifNodes=ifNodes.concat(Array.from(root.querySelectorAll ? root.querySelectorAll('[data-spl-if]') : []));
  ifNodes.forEach(function(el){
    if(el.__splIfBound){return;}
    el.__splIfBound=true;
    var name=el.getAttribute('data-spl-if');
    var update=function(value){
      el.style.display=Boolean(value)?'':'none';
    };
    update(SPL.read(name));
    SPL.subscribe(name,update);
  });
  var elseNodes=(root.matches && root.matches('[data-spl-else]'))?[root]:[];
  elseNodes=elseNodes.concat(Array.from(root.querySelectorAll ? root.querySelectorAll('[data-spl-else]') : []));
  elseNodes.forEach(function(el){
    if(el.__splElseBound){return;}
    el.__splElseBound=true;
    var name=el.getAttribute('data-spl-else');
    var update=function(value){
      el.style.display=Boolean(value)?'none':'';
    };
    update(SPL.read(name));
    SPL.subscribe(name,update);
  });
};`

const moduleRefs = `SPL.patchRefs=function(root){
  var nodes=(root.matches && root.matches('[data-spl-ref]'))?[root]:[];
  nodes=nodes.concat(Array.from(root.querySelectorAll ? root.querySelectorAll('[data-spl-ref]') : []));
  nodes.forEach(function(el){
    var name=el.getAttribute('data-spl-ref');
    if(name){SPL.refs[name]=el;}
  });
};`

// splBootstrapJS is the per-page initialization code that reads the payload
// and wires up signals, handlers, effects, and views.
const splBootstrapJS = `Object.keys(payload.signals||{}).forEach(function(name){
SPL.ensureSignal(name,payload.signals[name]);
});
Object.keys(payload.handlers||{}).forEach(function(name){
var expr=payload.handlers[name];
if(!expr){return;}
SPL.registerHandler(name, function(event, element){
return SPL.executeEvent(expr,event,element);
});
});
SPL.patch(document);
(payload.effects||[]).forEach(function(effect){
var target=document.querySelector(effect.Selector);
if(!target){return;}
var render=function(){
var focusSnapshot=SPL.captureFocus(target);
target.innerHTML=SPL.interpolate(effect.Source);
SPL.patch(target);
SPL.restoreFocus(target,focusSnapshot);
SPL.debugRecord('effect', effect.Selector);
};
render();
(effect.Deps||[]).forEach(function(dep){SPL.subscribe(dep,render);});
});
(payload.views||[]).forEach(function(view){
var target=document.querySelector(view.Selector);
if(!target){return;}
var render=function(){
var focusSnapshot=SPL.captureFocus(target);
target.innerHTML=SPL.interpolate(view.Source);
SPL.patch(target);
SPL.restoreFocus(target,focusSnapshot);
SPL.debugRecord('view', view.Selector);
};
render();
(view.Deps||[]).forEach(function(dep){SPL.subscribe(dep,render);});
});`

// ---------------------------------------------------------------------------
// JS Obfuscator — runs at init time, zero per-request cost
// ---------------------------------------------------------------------------

// genVarName generates short variable names: _a, _b, ..., _z, _aa, _ab, ...
func genVarName(n int) string {
	var name string
	for {
		name = string(rune('a'+(n%26))) + name
		n = n/26 - 1
		if n < 0 {
			break
		}
	}
	return "_" + name
}

// Internal SPL properties to mangle (not referenced in bootstrap or user code).
// Order is deterministic (sorted slice) so mangled names are stable.
var splInternalProps = []string{
	"apiParse",
	"applyBinding",
	"bindingEvent",
	"callHandler",
	"captureFocus",
	"debugRecord",
	"evalExpression",
	"expressionCache",
	"extractDeps",
	"getRenderStats",
	"getScope",
	"patchAPI",
	"patchBindings",
	"patchConditionals",
	"patchEvents",
	"patchModels",
	"patchRefs",
	"readBindingValue",
	"readPath",
	"reservedNames",
	"restoreFocus",
	"runStatement",
	"serializeForm",
	"signalName",
	"signalRef",
	"statementCache",
	"writePath",
}

// buildPropMap creates the deterministic property mangling map
func buildPropMap() map[string]string {
	m := make(map[string]string, len(splInternalProps))
	for i, prop := range splInternalProps {
		m[prop] = genVarName(i)
	}
	return m
}

// propManglingMap is computed once at init
var propManglingMap map[string]string

func init() {
	propManglingMap = buildPropMap()
}

// encodeString converts a string literal to a char-code array decoding expression.
// Short strings (< 8 chars) are left as-is.
func encodeString(s string) string {
	if len(s) < 8 {
		return "'" + s + "'"
	}
	var codes []string
	for _, c := range s {
		codes = append(codes, fmt.Sprintf("%d", c))
	}
	return fmt.Sprintf("([%s].map(function(c){return String.fromCharCode(c)}).join(''))", strings.Join(codes, ","))
}

// obfuscateJS applies obfuscation to a JS source string:
//   - Property mangling: SPL.internalProp → SPL._xx
//   - String encoding: 'data-spl-xxx' → charCode array
//   - Whitespace minification
func obfuscateJS(src string) string {
	result := src

	// 1. Mangle SPL.propertyName → SPL._xx for internal properties.
	// This is safe because we only match the exact "SPL." prefix.
	for orig, mangled := range propManglingMap {
		result = strings.ReplaceAll(result, "SPL."+orig, "SPL."+mangled)
	}

	// 2. Encode data-spl-* string literals to charCode arrays.
	// Only encode strings in single quotes that contain 'data-spl-'.
	result = encodeDataSPLStrings(result)

	// 3. Minify: collapse whitespace
	result = minifyJS(result)

	return result
}

// dataSPLStringRe matches single-quoted strings containing 'data-spl-'
var dataSPLStringRe = regexp.MustCompile(`'(data-spl-[^']*)'`)

func encodeDataSPLStrings(src string) string {
	return dataSPLStringRe.ReplaceAllStringFunc(src, func(match string) string {
		inner := match[1 : len(match)-1]
		return encodeString(inner)
	})
}

// minifyJS removes unnecessary whitespace from JS source.
// It's string-literal-aware to avoid breaking quoted content.
func minifyJS(src string) string {
	var sb strings.Builder
	sb.Grow(len(src))

	inString := byte(0) // 0, '\'' or '"'
	prevWasSpace := false
	prev := byte(0)

	for i := 0; i < len(src); i++ {
		c := src[i]

		// Inside string literal: pass through unchanged
		if inString != 0 {
			if c == inString && prev != '\\' {
				inString = 0
			}
			sb.WriteByte(c)
			prev = c
			prevWasSpace = false
			continue
		}

		// Entering string literal
		if c == '\'' || c == '"' || c == '`' {
			if prevWasSpace && sb.Len() > 0 {
				last := sb.String()[sb.Len()-1]
				if isWordChar(last) {
					sb.WriteByte(' ')
				}
			}
			prevWasSpace = false
			inString = c
			sb.WriteByte(c)
			prev = c
			continue
		}

		// Whitespace: collapse to at most one space
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			prevWasSpace = true
			continue
		}

		// Non-whitespace: emit deferred space if needed
		if prevWasSpace && sb.Len() > 0 {
			last := sb.String()[sb.Len()-1]
			if needsSpaceBetween(last, c) {
				sb.WriteByte(' ')
			}
			prevWasSpace = false
		}

		sb.WriteByte(c)
		prev = c
	}
	return sb.String()
}

func needsSpaceBetween(a, b byte) bool {
	return isWordChar(a) && isWordChar(b)
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$'
}

// ---------------------------------------------------------------------------
// Module assembly + tree-shaking
// ---------------------------------------------------------------------------

// buildPatchFunction generates the SPL.patch function based on included features
func buildPatchFunction(features jsFeature) string {
	var calls []string
	if features&featBindings != 0 {
		calls = append(calls, "SPL.patchBindings(scope);")
	}
	if features&featModels != 0 {
		calls = append(calls, "SPL.patchModels(scope);")
	}
	if features&featEvents != 0 {
		calls = append(calls, "SPL.patchEvents(scope);")
	}
	if features&featAPI != 0 {
		calls = append(calls, "SPL.patchAPI(scope);")
	}
	if features&featConditionals != 0 {
		calls = append(calls, "SPL.patchConditionals(scope);")
	}
	if features&featRefs != 0 {
		calls = append(calls, "SPL.patchRefs(scope);")
	}
	if len(calls) == 0 {
		return "SPL.patch=function(){};"
	}
	return "SPL.patch=function(root){var scope=root||document;" + strings.Join(calls, "") + "};"
}

// assembleRuntime builds the full runtime JS for the given feature set
func assembleRuntime(features jsFeature, disableDebug bool) string {
	var sb strings.Builder

	// Core + Scope are always included
	sb.WriteString(moduleCore)
	sb.WriteString("\n")
	sb.WriteString(moduleScope)
	sb.WriteString("\n")

	// Debug
	if disableDebug {
		sb.WriteString(moduleDebugStub)
	} else {
		sb.WriteString(moduleDebug)
	}
	sb.WriteString("\n")

	// Focus (needed when effects/views exist)
	if features&featFocus != 0 {
		sb.WriteString(moduleFocus)
	} else {
		sb.WriteString(moduleFocusStub)
	}
	sb.WriteString("\n")

	// Optional modules
	if features&featBindings != 0 {
		sb.WriteString(moduleBindings)
		sb.WriteString("\n")
	}
	if features&featEvents != 0 {
		sb.WriteString(moduleEvents)
		sb.WriteString("\n")
	}
	if features&featModels != 0 {
		sb.WriteString(moduleModels)
		sb.WriteString("\n")
	}
	if features&featAPI != 0 {
		sb.WriteString(moduleAPI)
		sb.WriteString("\n")
	}
	if features&featConditionals != 0 {
		sb.WriteString(moduleConditionals)
		sb.WriteString("\n")
	}
	if features&featRefs != 0 {
		sb.WriteString(moduleRefs)
		sb.WriteString("\n")
	}

	// Patch function (adapted to included modules)
	sb.WriteString(buildPatchFunction(features))

	return sb.String()
}

// detectFeatures scans rendered HTML and hydration sources for feature usage
func detectFeatures(renderedHTML string, effects []hydrationEffect, views []hydrationView) jsFeature {
	features := featCore | featScope // always needed

	// Effects or views exist → need focus
	if len(effects) > 0 || len(views) > 0 {
		features |= featFocus
	}

	// Scan each source individually to avoid allocating a joined string
	sources := make([]string, 0, 1+len(effects)+len(views))
	sources = append(sources, renderedHTML)
	for _, e := range effects {
		sources = append(sources, e.Source)
	}
	for _, v := range views {
		sources = append(sources, v.Source)
	}

	for _, src := range sources {
		if features&featBindings == 0 && strings.Contains(src, "data-spl-bind") {
			features |= featBindings
		}
		if features&featEvents == 0 && strings.Contains(src, "data-spl-on-") {
			features |= featEvents
		}
		if features&featModels == 0 && strings.Contains(src, "data-spl-model") {
			features |= featModels
		}
		if features&featAPI == 0 && strings.Contains(src, "data-spl-api-") {
			features |= featAPI
		}
		if features&featConditionals == 0 && (strings.Contains(src, "data-spl-if") || strings.Contains(src, "data-spl-else")) {
			features |= featConditionals
		}
		if features&featRefs == 0 && strings.Contains(src, "data-spl-ref") {
			features |= featRefs
		}
	}

	return features
}

// ---------------------------------------------------------------------------
// Obfuscated runtime cache
// ---------------------------------------------------------------------------

func getObfuscatedFull(disableDebug bool) string {
	raw := assembleRuntime(featAll, disableDebug)
	return obfuscateJS(raw)
}

// moduleCache caches obfuscated modules by feature bitmask
var moduleCache = struct {
	sync.RWMutex
	cache map[jsFeature]string
}{cache: make(map[jsFeature]string)}

func getObfuscatedForFeatures(features jsFeature, disableDebug bool) string {
	key := features
	if disableDebug {
		key |= 1 << 15
	}

	moduleCache.RLock()
	cached, ok := moduleCache.cache[key]
	moduleCache.RUnlock()
	if ok {
		return cached
	}

	raw := assembleRuntime(features, disableDebug)
	obfuscated := obfuscateJS(raw)

	moduleCache.Lock()
	moduleCache.cache[key] = obfuscated
	moduleCache.Unlock()

	return obfuscated
}

var obfuscatedBootstrap string

func init() {
	// Apply the same property mangling to bootstrap as to the runtime
	boot := splBootstrapJS
	for orig, mangled := range propManglingMap {
		boot = strings.ReplaceAll(boot, "SPL."+orig, "SPL."+mangled)
	}
	obfuscatedBootstrap = minifyJS(boot)
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// RuntimeJS returns the fully obfuscated SPL hydration runtime JavaScript
// with all features included. Serve this as a static .js file and set
// Engine.HydrationRuntimeURL to enable browser caching across pages.
func (e *Engine) RuntimeJS() string {
	return getObfuscatedFull(e.DisableDebug)
}

// RuntimeJSRaw returns the unobfuscated, minified SPL hydration runtime.
// Useful for debugging.
func (e *Engine) RuntimeJSRaw() string {
	raw := assembleRuntime(featAll, e.DisableDebug)
	return minifyJS(raw)
}
