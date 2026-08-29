# Interview Preparation: Architecture & Trade-offs

Yeh document NeuroFIQ project ke kuch deep-dive interview questions ko track karne ke liye hai, mainly Authentication, Sessions, aur Security ke around.

### 1. "Aapne JWT (JSON Web Token) ya LocalStorage ki jagah HTTP-Only Cookie Sessions kyun use kiye?"
**Aapka Answer:** 
"Security aur simplicity ki wajah se. Agar main token ko React ke `LocalStorage` mein rakhta, toh wo **XSS (Cross-Site Scripting)** attack se chori ho sakta tha. Agar frontend pe koi malicious script run ho jaye, toh wo LocalStorage read kar sakti hai. 
Iske against, maine Go mein `HTTP-Only` secure cookies use ki hain. Iska faida yeh hai ki browser khud-ba-khud cookie ko har request ke saath Go backend ko bhej deta hai, lekin frontend ka JavaScript us cookie ko touch ya read nahi kar sakta. Yeh MVP ke liye ekdum solid security approach hai."

### 2. "Aapka backend abhi Session use kar raha hai. Agar kal ko main iske 5 naye backend servers laga doon (Load Balancer ke peechay), toh kya yeh sessions fail ho jayenge?"
**Aapka Answer:** 
"Nahi, fail nahi honge, kyunki maine **Stateless Cookie Store** (`gin-contrib/sessions/cookie`) use kiya hai. 
Normal server-side sessions mein data server ki memory (RAM) mein hota hai, jiske liye Redis lagana padta hai. Lekin mere architecture mein, user ki ID aur token ko encrypt karke khud us cookie ke andar hi store kiya gaya hai (client-side par). Jab request aati hai, toh koi bhi 5 mein se ek server us cookie ko ek shared `SESSION_SECRET` (environment variable) use karke decrypt kar sakta hai. Isliye yeh approach perfectly horizontally scalable hai bina kisi extra Redis database ke."

### 3. "GitHub OAuth ka exact flow samjhao jo tumne implement kiya hai. Yeh secure kaise hai?"
**Aapka Answer:** 
"Maine standard **Authorization Code Flow** implement kiya hai:
1. User frontend se Go backend ke `/auth/github/login` pe jaata hai.
2. Go backend user ko GitHub ke official page par redirect karta hai mere `Client_ID` ke sath.
3. User GitHub par approve karta hai, toh GitHub use mere `/callback` route par ek chote se **'Code'** (temp pass) ke sath wapas bhejta hai.
4. Mera Go backend us 'Code' aur mere `Client_Secret` ko lekar *server-to-server* (backend background mein) GitHub se baat karta hai aur actual Access Token fetch karta hai.
5. Phir Go us token ko secure cookie me rakh kar user ko React ke dashboard pe redirect kar deta hai. 

**Yeh secure isliye hai:** Kyunki actual *Access Token* kabhi frontend ko nahi milta aur URL mein expose nahi hota, sirf server-to-server exchange hota hai. Client Secret kabhi browser ke paas nahi jata."

---
*💡 Interview Tip: Jab aap is tarah answers dete ho (jismein aap Security, XSS Attacks, Stateless Scaling, aur Server-to-Server communication jaise words use karte ho), toh interviewer ko samajh aa jata hai ki aapne sirf code copy-paste nahi kiya, balki system design aur production problems ko samajh kar faisla liya hai.*
