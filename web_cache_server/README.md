
# WebCacheServer 동작 방식
                                                                    
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓                                      ┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ ┏━━━━━━━━┓     ┏━━━━━━━━━━━━━┓ ┃                                 ┏━━> ┃             Server            ┃
┃ ┃ Client ┃ ━━> ┃ Virtual Box ┃ ┃      ┏━━━━━━━━━━━━━━━━━━━━━━┓   ┃    ┃  http://global.gmarket.co.kr  ┃
┃ ┗━━━━━━━━┛     ┗━━━━━━━━━━━━━┛ ┃ ━━━> ┃ Reverse Proxy Server ┃ ━━┫    ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛ 
┃            ┏━━━━━━━━┓          ┃      ┗━━━━━━━━━━━━━━━━━━━━━━┛   ┃    ┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓ 
┃       ━━━> ┃ Docker ┃          ┃                                 ┃    ┃             Server            ┃     
┃            ┗━━━━━━━━┛          ┃                                 ┗━━> ┃   http://image.gmarket.co.kr  ┃         
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛                                      ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛                                 
                                                                                               






# ResoponseCode에 따른 Cache Control

저장
- 200 : OK
    성공적인 응답이므로 웹 캐시에 저장.

미저장
- 203 : Non-Authoritative Information
    중개 서버가 캐시에 대한 권한을 가지지 않으므로 저장하지 않음.
- 206 : Partial Content
    클라이언트가 요청의 일부만 성공적으로 처리했으므로 저장하지 않음.
- 301 : Moved Permanently
    요청한 페이지를 새 위치로 영구적으로 이동. 요청자가 자동으로 새 위치로 전달됨. 캐시를 저장하지 않음.
- 302 : Found (임시 이동)
    현재 서버가 다른 위치의 페이지로 요청에 응답하고 있지만 요청자는 향후 요청 시 원래 위치를 계속 사용해야 함. 캐시를 저장하지 않음.
- 304 : Not Modified
    클라이언트가 이미 캐시된 리소스의 최신 버전을 가지고 있음. 캐시를 저장할 필요 없음.
- 307 : Temporary Redirect
    임시 리다이렉션. 리다이렉션은 캐시를 저장하지 않음.
    



# Cache-Control에 따른 Cache Control

미저장 (해당 문자열을 포함하는 경우)
- "no-store"
    캐시를 저장하지 않도록 지시하는 것.
- "no-cache"
    데이터를 사용하기 전에 서버에 확인해야 하므로 저장하지 않음.
- "proxy-revalidate"
    중간 프록시 서버에서 캐시된 데이터를 사용하기 전에 서버에 확인해야 하므로 저장하지 않음.
- "private"
    개별 사용자 혹은 사용자 그룹의 개인 캐시에만 저장해야 하므로 저장하지 않음.

저장
- 그 외




# Content-Type에 따른 Cache Control

저장 (해당 문자열로 시작하는 경우)
- "text/"
    문자열 저장
- "image/"
    이미지 파일 저장

미저장
- 그 외




# Config 옵션

- MaxFileSize (int)
    캐시를 저장할 때 두는 파일 크기 제한
- GzipEnabled (bool)
    캐시된 데이터를 Gzip 형식으로 압축해서 보내는 기능
    true 일 때 압축, false 일 때 압축X
    (단, Client의 Accept-Encoding에 gzip이 없다면 압축하지 않음)
- CacheExceptions (string-array)
    정규표현식의 배열로 이루어져 있으며, Response된 url이 match될 경우 캐시하지 않음
- QueryIgnoreEnabled (bool)
    캐시 데이터 저장 시 특정 데이터를 sha256으로 변환해 저장하는데, 데이터에 Query를 포함할지에 대한 기능
    true 일 때 미포함, false 일 때 포함
- QuerySortingEnabled (bool)
    캐시 데이터 저장 시 Query를 포함한 데이터를 sha256으로 변환하는데, 이 때 Query들의 key를 기준으로 sort할지에 대한 기능
    true 일 때 정렬, false 일 때 정렬x
- ResponseTimeLoggingEnabled (bool)
    통신이 이루어질 때 각 통신의 response에 걸린 시간을 log파일에 저장. 캐시된 데이터를 보낼 때도 저장.
    true 일 때 저장, false 일 때 저장x
- CleanupFrequency (int)
    유효시간이 만료된 캐시 데이터의 삭제 빈도. 초 단위.
    60일 경우, 1분마다 만료된 캐시를 삭제함




# 보낸 데이터가 캐시 데이터인지 확인 방법

브라우저의 Developder Tool(F12키)의 네트워크 탭에서 항목들의 Response Headers에 "Jnlee : HIT" 가 있는지 확인

