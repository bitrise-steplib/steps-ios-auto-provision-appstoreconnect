require_relative 'portal/auth_client'
require_relative 'certificate_helper'
require_relative 'log'

begin
  Log.verbose = true
  Portal::AuthClient.login(apple_id, password)
  Log.info('logged in')

  client = CertificateHelper.new
  certificates = client.identify_certificate_infos
  result = { data: certificates }
  puts result.to_json.to_s
rescue => ex
  puts
  Log.debug_exception(ex)

  exit 1
end
