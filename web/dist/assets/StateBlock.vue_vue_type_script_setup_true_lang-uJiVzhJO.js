import{c,d as m,a as o,b as i,m as s,f as n,an as y,t as r,i as l,_ as u,w as k,s as t,e as x,ac as g,h as p}from"./index-DyR35w6g.js";/**
 * @license @lucide/vue v1.25.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const b=[["polyline",{points:"22 12 16 12 14 15 10 15 8 12 2 12",key:"o97t9d"}],["path",{d:"M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z",key:"oot6mr"}]],h=c("inbox",b);/**
 * @license @lucide/vue v1.25.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const f=[["path",{d:"m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3",key:"wmoenq"}],["path",{d:"M12 9v4",key:"juzpu7"}],["path",{d:"M12 17h.01",key:"p32p05"}]],v=c("triangle-alert",f),z={class:"panel grid min-h-52 place-items-center p-8 text-center"},B={class:"font-extrabold"},C={key:3,class:"muted mt-2 max-w-lg text-sm"},V=m({__name:"StateBlock",props:{state:{},title:{},description:{},retrying:{type:Boolean}},emits:["retry"],setup(e){return(d,a)=>(t(),o("div",z,[i("div",null,[e.state==="loading"?(t(),s(n(y),{key:0,class:"mx-auto mb-4 animate-spin text-[var(--brand)]",size:30})):e.state==="error"?(t(),s(n(v),{key:1,class:"mx-auto mb-4 text-[var(--danger)]",size:30})):(t(),s(n(h),{key:2,class:"muted mx-auto mb-4",size:32})),i("h3",B,r(e.title||(e.state==="loading"?"正在加载":"这里还没有内容")),1),e.description?(t(),o("p",C,r(e.description),1)):l("",!0),e.state==="error"?(t(),s(u,{key:4,class:"btn btn-secondary mt-5",loading:e.retrying,"loading-label":"重试中…",onClick:a[0]||(a[0]=N=>d.$emit("retry"))},{default:k(()=>[x(n(g),{size:16}),a[1]||(a[1]=p("重试",-1))]),_:1},8,["loading"])):l("",!0)])]))}});export{v as T,V as _};
//# sourceMappingURL=StateBlock.vue_vue_type_script_setup_true_lang-uJiVzhJO.js.map
